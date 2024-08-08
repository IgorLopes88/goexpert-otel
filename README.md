# Go Expert

Desafio **Observalidade & Open Telemetry** do curso **Pós Go Expert**.

**Objetivo:** Desenvolver um sistema em Go que receba um CEP, identifica a cidade e retorna o clima atual (temperatura em graus celsius, fahrenheit e kelvin) juntamente com a cidade. Esse sistema deverá implementar OTEL(Open Telemetry) e Zipkin..

### Execução da **aplicação**
Para executar a aplicação execute o comando:
```
git clone https://github.com/IgorLopes88/goexpert-otel.git
cd goexpert-otel
docker compose up --build
```

O resultado deverá ser esse:

```
 ✔ Container otel-collector  Created
 ✔ Container zipkin          Created
 ✔ Container service_b       Created 
 ✔ Container service_a       Created
```

Utilize o arquivo `test.http` para acessar os serviços A e B.

Para acessar o **ZIPKIN**, abra o navegador e acesse `http://localhost:9411/zipkin`.

### Evidência do Funcionamento

Segue print do funcionamento do sistema:

![Informação Coletada sobre ](/image/img01.png)

De cima para baixo, segue a descrição de cada linha:
 1. `service_a: /temperature` - Informação sobre a requisição completa;
 2. `service_a: handler` - Informação sobre a execução do handler do serviço A;
 3. `service_a: http get` - Informação sobre a requisição enviada para o serviço B;
 4. `service_b: /temperature/{zipcode}` - Informação sobre a requisição completa do serviço B;
 5. `service_b: handler` - Informação sobre a execução do handler do serviço B;
 6. `service_b: http get` - Informação sobre a requisição enviada para a API ViaCEP;
 7. `service_b: http get` - Informação sobre a requisição enviada para a API WeatherApi;

Pronto!


### Correções de Bugs
1. Anexo de evidência e re-testes;
2. Resolvendo erro: `Error: failed to get config: cannot resolve the configuration: cannot retrieve the configuration: unable to read the file file:/etc/otel-collector-config.yaml: read /etc/otel-collector-config.yaml: is a directory`