package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

type ZipCodeStruct struct {
	ZipCode string `json:"cep"`
}

type TemperatureStruct struct {
	City       string  `json:"city"`
	Celsius    float64 `json:"temp_C"`
	Fahrenheit float64 `json:"temp_F"`
	Kelvin     float64 `json:"temp_K"`
}

var Tracer = otel.Tracer("microservice-tracer")
var CollectorURL = "otel-collector:4317"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	shutdown, err := InitTracer(ctx, "service_a")
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := shutdown(context.Background()); err != nil {
			panic(err)
		}
	}()

	router := mux.NewRouter()
	router.Use(otelmux.Middleware("service_a"))
	router.HandleFunc("/temperature", GetTemperature).Methods("POST")
	http.ListenAndServe(":8080", router)
}

func GetTemperature(w http.ResponseWriter, r *http.Request) {
	carrier := propagation.HeaderCarrier(r.Header)
	ctx := r.Context()
	ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)

	ctx, span := Tracer.Start(ctx, "handler")
	defer span.End()

	w.Header().Set("Content-Type", "application/json")

	var req ZipCodeStruct
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		log.Print(err)
	}

	err = convertZipcode(req.ZipCode)
	if err != nil {
		w.Header().Set("Content-Type", "application/text")
		w.WriteHeader(422)
		w.Write([]byte("invalid zipcode"))
		log.Printf("Invalid Zipcode: %s", req)
		return
	}

	result, err := requestGetUrl(ctx, "http://service_b:8081/temperature/"+req.ZipCode)
	if err != nil {
		w.Header().Set("Content-Type", "application/text")
		w.WriteHeader(422)
		w.Write([]byte("invalid zipcode"))
		log.Printf("Invalid Zipcode: %s", req)
		return
	}

	var data TemperatureStruct
	err = json.Unmarshal(result, &data)
	if err != nil {
		w.Header().Set("Content-Type", "application/text")
		w.WriteHeader(404)
		w.Write([]byte("can not find zipcode"))
		log.Printf("Can not find zipcode: %s", req)
		return
	}

	if data.City == "" {
		w.Header().Set("Content-Type", "application/text")
		w.WriteHeader(404)
		w.Write([]byte("can not find zipcode"))
		log.Printf("Can not find zipcode: %s", req)
		return
	}

	celsius := strconv.FormatFloat(data.Celsius, 'f', 1, 64)
	fahrenheit := strconv.FormatFloat(data.Fahrenheit, 'f', 1, 64)
	kelvin := strconv.FormatFloat(data.Kelvin, 'f', 1, 64)

	responseJson := []byte(`{ "city": "` + data.City + `", "temp_C": ` + celsius + `, "temp_F": ` + fahrenheit + `, "temp_K": ` + kelvin + ` }`)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(responseJson)
	log.Printf("Zipcode Request: %s (%s): %vC / %vF / %vK ", req.ZipCode, data.City, celsius, fahrenheit, kelvin)
}

func requestGetUrl(c context.Context, url string) ([]byte, error) {
	res, _ := otelhttp.Get(c, url)
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		return nil, err
	}
	return body, nil
}

func convertZipcode(zipcode string) error {
	zipcode = strings.Replace(zipcode, "-", "", 1)
	match, err := regexp.MatchString("[0-9]", zipcode)
	if err != nil {
		return errors.New("invalid zipcode")
	}
	if !match {
		return errors.New("invalid zipcode")
	}
	if zipcode == "" || len(zipcode) != 8 {
		return errors.New("invalid zipcode")
	}
	return nil
}

func InitTracer(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed: %w", err)
	}
	conn, err := grpc.NewClient(CollectorURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed exporter: %w", err)
	}
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetTracerProvider(tracerProvider)
	return tracerProvider.Shutdown, nil
}
