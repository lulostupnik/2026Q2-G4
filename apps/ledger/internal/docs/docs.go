package docs

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var spec []byte

const specPath = "/openapi.yaml"
const uiPath = "/docs"

const specContentType = "application/yaml"

const swaggerUIVersion = "5.17.14"

const uiTemplate = `<!doctype html>
<html lang="es">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Núcleo Transaccional — API</title>
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/` + swaggerUIVersion + `/swagger-ui.min.css">
  <style>
    body { margin: 0; background: #fafafa; }
    /* La barra de exploracion deja escribir otra URL de spec: en una demo solo sirve para
       romper la pagina sin querer. */
    .swagger-ui .topbar { display: none; }
    .swagger-ui .info { margin: 24px 0 12px; }
    .swagger-ui .info .title { font-size: 30px; }
    .swagger-ui .info table { border-collapse: collapse; margin: 12px 0; font-size: 13px; }
    .swagger-ui .info table td { border: 1px solid #e0e0e0; padding: 6px 10px; }
    .swagger-ui .info table td:first-child { background: #f7f7f7; white-space: nowrap; }
    .swagger-ui .scheme-container { display: none; }
    /* El selector de servidores, tanto el global como el que Swagger dibuja dentro de una
       operacion. La API se consume contra el origen que la sirve y no hay nada que elegir. */
    .swagger-ui .servers, .swagger-ui .servers-title { display: none; }
    .swagger-ui .opblock-tag { font-size: 20px; }
    .swagger-ui .opblock-tag small { font-size: 13px; }
  </style>
</head>
<body>
  <div id="swagger"></div>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/swagger-ui/` + swaggerUIVersion + `/swagger-ui-bundle.min.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: "` + specPath + `",
      dom_id: "#swagger",
      deepLinking: true,
      tryItOutEnabled: true,
      docExpansion: "none",
      filter: true,
      defaultModelsExpandDepth: -1,
      defaultModelExpandDepth: 3,
      displayRequestDuration: true
    });
  </script>
</body>
</html>`

func Handler() (specHandler, uiHandler http.HandlerFunc) {
	return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", specContentType)
			_, _ = w.Write(spec)
		}, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(uiTemplate))
		}
}

func Paths() (spec, ui string) { return specPath, uiPath }
