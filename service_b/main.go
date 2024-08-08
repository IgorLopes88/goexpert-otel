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
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

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

type WeatherApi struct {
	Location struct {
		Name           string  `json:"name"`
		Region         string  `json:"region"`
		Country        string  `json:"country"`
		Lat            float64 `json:"lat"`
		Lon            float64 `json:"lon"`
		TzID           string  `json:"tz_id"`
		LocaltimeEpoch int     `json:"localtime_epoch"`
		Localtime      string  `json:"localtime"`
	} `json:"location"`
	Current struct {
		LastUpdatedEpoch int     `json:"last_updated_epoch"`
		LastUpdated      string  `json:"last_updated"`
		TempC            float64 `json:"temp_c"`
		TempF            float64 `json:"temp_f"`
		IsDay            int     `json:"is_day"`
		Condition        struct {
			Text string `json:"text"`
			Icon string `json:"icon"`
			Code int    `json:"code"`
		} `json:"condition"`
		WindMph    float64 `json:"wind_mph"`
		WindKph    float64 `json:"wind_kph"`
		WindDegree int     `json:"wind_degree"`
		WindDir    string  `json:"wind_dir"`
		PressureMb float64 `json:"pressure_mb"`
		PressureIn float64 `json:"pressure_in"`
		PrecipMm   float64 `json:"precip_mm"`
		PrecipIn   float64 `json:"precip_in"`
		Humidity   int     `json:"humidity"`
		Cloud      int     `json:"cloud"`
		FeelslikeC float64 `json:"feelslike_c"`
		FeelslikeF float64 `json:"feelslike_f"`
		VisKm      float64 `json:"vis_km"`
		VisMiles   float64 `json:"vis_miles"`
		Uv         float64 `json:"uv"`
		GustMph    float64 `json:"gust_mph"`
		GustKph    float64 `json:"gust_kph"`
	} `json:"current"`
}

type ViaCEP struct {
	Cep          string `json:"cep"`
	State        string `json:"uf"`
	City         string `json:"localidade"`
	Neighborhood string `json:"bairro"`
	Street       string `json:"logradouro"`
	Complemento  string `json:"complemento"`
	Ibge         string `json:"ibge"`
	Gia          string `json:"gia"`
	DDD          string `json:"ddd"`
	Siafi        string `json:"siafi"`
}

var ApiKey = "466f39dc1a8848d8977124707242407"
var Tracer = otel.Tracer("microservice-tracer")
var CollectorURL = "otel-collector:4317"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	shutdown, err := InitTracer(ctx, "service_b")
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := shutdown(context.Background()); err != nil {
			panic(err)
		}
	}()

	router := mux.NewRouter()
	router.Use(otelmux.Middleware("service_b"))
	router.HandleFunc("/temperature/{zipcode}", handlerTemperature).Methods("GET")
	http.ListenAndServe(":8081", router)
}

func handlerTemperature(w http.ResponseWriter, r *http.Request) {
	carrier := propagation.HeaderCarrier(r.Header)
	ctx := r.Context()
	ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
	ctx, span := Tracer.Start(ctx, "handler")
	defer span.End()

	params := mux.Vars(r)
	request := params["zipcode"]
	zipcode, err := convertZipcode(request)
	if err != nil || zipcode == "" {
		w.WriteHeader(422)
		w.Write([]byte("invalid zipcode"))
		log.Printf("Invalid Zipcode Request: %s", request)
		return
	}

	city, err := SearchLocation(ctx, zipcode)
	if err != nil || city == "" {
		w.WriteHeader(404)
		w.Write([]byte("can not find zipcode"))
		log.Printf("City Not Located: %s", zipcode)
		return
	}

	response, err := GetTemperature(ctx, city)
	if err != nil {
		w.WriteHeader(404)
		w.Write([]byte("can not find zipcode"))
		log.Print(err)
		return
	}

	celsius := strconv.FormatFloat(response, 'f', 1, 64)
	fahrenheit := strconv.FormatFloat(response*1.8+32, 'f', 1, 64)
	kelvin := strconv.FormatFloat(response+273.15, 'f', 1, 64)

	responseJson := []byte(`{ "city": "` + city + `", "temp_C": ` + celsius + `, "temp_F": ` + fahrenheit + `, "temp_K": ` + kelvin + ` }`)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(responseJson)
	log.Printf("Zipcode Request: %s (%s): %vC / %vF / %vK ", zipcode, city, celsius, fahrenheit, kelvin)
}

func SearchLocation(ctx context.Context, cep string) (string, error) {
	result, err := requestGetUrl(ctx, "http://viacep.com.br/ws/"+cep+"/json")
	if err != nil {
		return "", err
	}

	var data ViaCEP
	err = json.Unmarshal(result, &data)
	if err != nil {
		return "", err
	}

	if data.Cep != "" {
		return data.City, nil
	} else {
		return "", errors.New("can not find zipcode")
	}
}

func GetTemperature(ctx context.Context, city string) (float64, error) {
	city = convertName(city)

	result, err := requestGetUrl(ctx, "http://api.weatherapi.com/v1/current.json?key="+ApiKey+"&q="+city+"&aqi=no")
	if err != nil {
		return 0, errors.New("city not found")
	}

	var u WeatherApi
	err = json.Unmarshal(result, &u)
	if err != nil {
		return 0, errors.New("city not found")
	}
	return u.Current.TempC, err
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

func convertZipcode(zipcode string) (string, error) {
	zipcode = strings.Replace(zipcode, "-", "", 1)
	match, err := regexp.MatchString("[0-9]", zipcode)
	if err != nil {
		return "", err
	}
	if !match {
		return "", errors.New("invalid zipcode")
	}
	if zipcode == "" || len(zipcode) != 8 {
		return "", errors.New("invalid zipcode")
	}
	return zipcode, nil
}

func convertName(name string) string {
	format := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(format, name)
	if err != nil {
		return ""
	}
	result = strings.Replace(result, " ", "%20", -1)
	return result
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
