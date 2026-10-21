package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DBURL  string
	Puerto string

	Bucket string
	Region string

	ObjetosDir string

	ProveedorURL     string
	ProveedorTimeout time.Duration

	ProveedorClave string

	CorsOrigenes string

	CognitoIssuer   string
	CognitoClientID string

	AuthAbierta bool

	MockClave string

	MockAbierto bool

	MockDemoraSinRespuesta time.Duration

	AdminClave string

	AdminAbierto bool

	Bienvenida int64

	ConciliacionIntervalo time.Duration

	WebhookURL      string
	WebhookSecreto  string
	WebhookIntentos int
	WebhookEspera   time.Duration

	RetencionMeses     int
	ArchivadoIntervalo time.Duration
}

func Cargar() (Config, error) {
	c := Config{
		DBURL:                  os.Getenv("DB_URL"),
		Puerto:                 texto("APP_PORT", "8080"),
		Bucket:                 os.Getenv("S3_BUCKET"),
		Region:                 texto("AWS_REGION", "us-east-1"),
		ObjetosDir:             os.Getenv("OBJETOS_DIR"),
		ProveedorURL:           os.Getenv("PROVEEDOR_URL"),
		ProveedorTimeout:       duracion("PROVEEDOR_TIMEOUT", 10*time.Second),
		ProveedorClave:         os.Getenv("PROVEEDOR_API_KEY"),
		CorsOrigenes:           texto("CORS_ORIGENES", "*"),
		CognitoIssuer:          os.Getenv("COGNITO_ISSUER"),
		CognitoClientID:        os.Getenv("COGNITO_CLIENT_ID"),
		AuthAbierta:            os.Getenv("AUTH_ABIERTA") == "1",
		MockClave:              os.Getenv("MOCK_API_KEY"),
		MockDemoraSinRespuesta: duracion("MOCK_DEMORA_SIN_RESPUESTA", 30*time.Second),
		MockAbierto:            os.Getenv("MOCK_ABIERTO") == "1",
		AdminClave:             os.Getenv("ADMIN_API_KEY"),
		AdminAbierto:           os.Getenv("ADMIN_ABIERTO") == "1",
		Bienvenida:             int64(entero("SALDO_BIENVENIDA_CENTAVOS", 5000000)),
		ConciliacionIntervalo:  duracion("CONCILIACION_INTERVALO", 30*time.Second),
		WebhookURL:             os.Getenv("WEBHOOK_URL"),
		WebhookSecreto:         os.Getenv("WEBHOOK_SECRET"),
		WebhookIntentos:        entero("WEBHOOK_MAX_ATTEMPTS", 3),
		WebhookEspera:          duracion("WEBHOOK_BACKOFF", 200*time.Millisecond),
		RetencionMeses:         entero("ARCHIVADO_RETENCION_MESES", 12),
		ArchivadoIntervalo:     duracion("ARCHIVADO_INTERVALO", 30*time.Second),
	}
	if c.DBURL == "" {
		return c, fmt.Errorf("falta DB_URL")
	}
	if c.Bucket == "" && c.ObjetosDir == "" {
		return c, fmt.Errorf("falta S3_BUCKET (o OBJETOS_DIR para correr local)")
	}

	if c.Bienvenida < 0 {
		return c, fmt.Errorf("SALDO_BIENVENIDA_CENTAVOS no puede ser negativo")
	}

	if c.WebhookURL != "" && c.WebhookSecreto == "" {
		return c, fmt.Errorf("hay WEBHOOK_URL pero falta WEBHOOK_SECRET")
	}
	return c, nil
}

func texto(clave, porDefecto string) string {
	if v := os.Getenv(clave); v != "" {
		return v
	}
	return porDefecto
}

func duracion(clave string, porDefecto time.Duration) time.Duration {
	v := os.Getenv(clave)
	if v == "" {
		return porDefecto
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return porDefecto
	}
	return d
}

func entero(clave string, porDefecto int) int {
	v := os.Getenv(clave)
	if v == "" {
		return porDefecto
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return porDefecto
	}
	return n
}

func (c Config) ValidarAPI() error {

	if c.MockClave == "" && !c.MockAbierto {
		return fmt.Errorf("falta MOCK_API_KEY: /servicio-mock queda expuesto y acredita " +
			"dinero. Configurala, o pone MOCK_ABIERTO=1 para desarrollo local")
	}

	if c.AdminClave == "" && !c.AdminAbierto {
		return fmt.Errorf("falta ADMIN_API_KEY: /admin/* queda expuesto y escribe en el " +
			"ledger. Configurala, o pone ADMIN_ABIERTO=1 para desarrollo local")
	}

	if (c.CognitoIssuer == "" || c.CognitoClientID == "") && !c.AuthAbierta {
		return fmt.Errorf("faltan COGNITO_ISSUER y COGNITO_CLIENT_ID: sin ellos no hay sesiones y " +
			"las rutas de negocio quedan abiertas. Configuralos, o pone AUTH_ABIERTA=1 para desarrollo local")
	}
	return nil
}
