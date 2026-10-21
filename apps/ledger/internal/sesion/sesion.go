package sesion

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalido = errors.New("token invalido")
	ErrVencido  = errors.New("token vencido")
)

const recargaMinima = time.Minute

type Identidad struct {
	Sub    string
	Expira time.Time
}

type Validador struct {
	issuer   string
	clientID string
	cliente  *http.Client

	mu       sync.Mutex
	claves   map[string]*rsa.PublicKey
	cargadas time.Time
}

func Nuevo(issuer, clientID string) *Validador {
	return &Validador{
		issuer:   strings.TrimRight(issuer, "/"),
		clientID: clientID,
		cliente:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (v *Validador) Habilitado() bool { return v != nil && v.issuer != "" && v.clientID != "" }

func (v *Validador) Issuer() string { return v.issuer }

type cabecera struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type cuerpo struct {
	Sub      string `json:"sub"`
	Iss      string `json:"iss"`
	Exp      int64  `json:"exp"`
	TokenUse string `json:"token_use"`
	ClientID string `json:"client_id"`
}

func (v *Validador) Validar(ctx context.Context, token string) (Identidad, error) {
	if !v.Habilitado() {
		return Identidad{}, errors.New("faltan COGNITO_ISSUER y COGNITO_CLIENT_ID: no se pueden validar sesiones")
	}

	partes := strings.Split(token, ".")
	if len(partes) != 3 {
		return Identidad{}, fmt.Errorf("%w: no tiene tres partes", ErrInvalido)
	}

	var cab cabecera
	if err := decodificarJSON(partes[0], &cab); err != nil {
		return Identidad{}, fmt.Errorf("%w: cabecera ilegible", ErrInvalido)
	}
	if cab.Alg != "RS256" {
		return Identidad{}, fmt.Errorf("%w: alg %q no es RS256", ErrInvalido, cab.Alg)
	}

	firma, err := base64.RawURLEncoding.DecodeString(partes[2])
	if err != nil {
		return Identidad{}, fmt.Errorf("%w: firma ilegible", ErrInvalido)
	}
	clave, err := v.clave(ctx, cab.Kid)
	if err != nil {
		return Identidad{}, err
	}
	suma := sha256.Sum256([]byte(partes[0] + "." + partes[1]))
	if err := rsa.VerifyPKCS1v15(clave, crypto.SHA256, suma[:], firma); err != nil {
		return Identidad{}, fmt.Errorf("%w: la firma no coincide", ErrInvalido)
	}

	var c cuerpo
	if err := decodificarJSON(partes[1], &c); err != nil {
		return Identidad{}, fmt.Errorf("%w: cuerpo ilegible", ErrInvalido)
	}
	switch {
	case c.Iss != v.issuer:
		return Identidad{}, fmt.Errorf("%w: iss %q no es el del user pool", ErrInvalido, c.Iss)
	case c.TokenUse != "access":
		return Identidad{}, fmt.Errorf("%w: token_use %q no es access", ErrInvalido, c.TokenUse)
	case c.ClientID != v.clientID:
		return Identidad{}, fmt.Errorf("%w: client_id %q no es el del app client", ErrInvalido, c.ClientID)
	case c.Sub == "":
		return Identidad{}, fmt.Errorf("%w: no tiene sub", ErrInvalido)
	case c.Exp <= 0:
		return Identidad{}, fmt.Errorf("%w: no tiene exp", ErrInvalido)
	}

	expira := time.Unix(c.Exp, 0)
	if time.Now().After(expira) {
		return Identidad{}, ErrVencido
	}
	return Identidad{Sub: c.Sub, Expira: expira}, nil
}

func (v *Validador) clave(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if k, ok := v.claves[kid]; ok {
		return k, nil
	}
	if time.Since(v.cargadas) < recargaMinima {
		return nil, fmt.Errorf("%w: kid %q desconocido", ErrInvalido, kid)
	}

	claves, err := v.cargar(ctx)
	if err != nil {
		return nil, err
	}
	v.claves, v.cargadas = claves, time.Now()

	if k, ok := claves[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("%w: kid %q desconocido", ErrInvalido, kid)
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func (v *Validador) cargar(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	pedido, err := http.NewRequestWithContext(ctx, http.MethodGet, v.issuer+"/.well-known/jwks.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.cliente.Do(pedido)
	if err != nil {
		return nil, fmt.Errorf("bajar JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bajar JWKS: status %d", resp.StatusCode)
	}

	var j jwks
	if err := json.NewDecoder(resp.Body).Decode(&j); err != nil {
		return nil, fmt.Errorf("leer JWKS: %w", err)
	}

	claves := make(map[string]*rsa.PublicKey, len(j.Keys))
	for _, k := range j.Keys {
		n, errN := base64.RawURLEncoding.DecodeString(k.N)
		e, errE := base64.RawURLEncoding.DecodeString(k.E)
		if k.Kty != "RSA" || errN != nil || errE != nil {
			continue
		}
		claves[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	return claves, nil
}

func decodificarJSON(parte string, destino any) error {
	crudo, err := base64.RawURLEncoding.DecodeString(parte)
	if err != nil {
		return err
	}
	return json.Unmarshal(crudo, destino)
}

func DelEncabezado(valor string) string {
	const esquema = "bearer "
	if len(valor) <= len(esquema) || !strings.EqualFold(valor[:len(esquema)], esquema) {
		return ""
	}
	return strings.TrimSpace(valor[len(esquema):])
}
