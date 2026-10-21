package sesion

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const clientID = "cliente-web"

type pool struct {
	clave    *rsa.PrivateKey
	servidor *httptest.Server
	pedidos  int
}

func nuevoPool(t *testing.T) *pool {
	t.Helper()
	clave, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &pool{clave: clave}
	p.servidor = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.pedidos++
		if r.URL.Path != "/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "k1",
			"kty": "RSA",
			"alg": "RS256",
			"n":   b64(clave.N.Bytes()),
			"e":   b64(big.NewInt(int64(clave.E)).Bytes()),
		}}})
	}))
	t.Cleanup(p.servidor.Close)
	return p
}

func (p *pool) validador() *Validador { return Nuevo(p.servidor.URL, clientID) }

func (p *pool) claims() map[string]any {
	return map[string]any{
		"sub":       "1b2c3d4e",
		"iss":       p.servidor.URL,
		"exp":       time.Now().Add(time.Hour).Unix(),
		"token_use": "access",
		"client_id": clientID,
	}
}

func (p *pool) firmar(t *testing.T, cab map[string]any, claims map[string]any, clave *rsa.PrivateKey) string {
	t.Helper()
	crudoCab, _ := json.Marshal(cab)
	crudoClaims, _ := json.Marshal(claims)
	firmable := b64(crudoCab) + "." + b64(crudoClaims)
	suma := sha256.Sum256([]byte(firmable))
	firma, err := rsa.SignPKCS1v15(rand.Reader, clave, crypto.SHA256, suma[:])
	if err != nil {
		t.Fatal(err)
	}
	return firmable + "." + b64(firma)
}

func (p *pool) token(t *testing.T, cambios map[string]any) string {
	claims := p.claims()
	for k, v := range cambios {
		claims[k] = v
	}
	return p.firmar(t, map[string]any{"alg": "RS256", "kid": "k1"}, claims, p.clave)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func TestTokenValido(t *testing.T) {
	p := nuevoPool(t)
	id, err := p.validador().Validar(context.Background(), p.token(t, nil))
	if err != nil {
		t.Fatalf("validar: %v", err)
	}
	if id.Sub != "1b2c3d4e" || time.Until(id.Expira) < 59*time.Minute {
		t.Fatalf("identidad inesperada: %+v", id)
	}
}

func TestClaimsQueNoCorresponden(t *testing.T) {
	p := nuevoPool(t)
	casos := map[string]map[string]any{
		"id token":        {"token_use": "id"},
		"otro app client": {"client_id": "otro"},
		"otro user pool":  {"iss": "https://cognito-idp.us-east-1.amazonaws.com/otro"},
		"sin sub":         {"sub": ""},
		"sin exp":         {"exp": 0},
	}
	for nombre, cambios := range casos {
		if _, err := p.validador().Validar(context.Background(), p.token(t, cambios)); !errors.Is(err, ErrInvalido) {
			t.Errorf("%s: deberia ser invalido, dio %v", nombre, err)
		}
	}
}

func TestVencidoSeDistingue(t *testing.T) {
	p := nuevoPool(t)
	token := p.token(t, map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})
	if _, err := p.validador().Validar(context.Background(), token); !errors.Is(err, ErrVencido) {
		t.Fatalf("deberia ser ErrVencido, dio %v", err)
	}
}

func TestFirmaDeOtraClave(t *testing.T) {
	p := nuevoPool(t)
	otra, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	token := p.firmar(t, map[string]any{"alg": "RS256", "kid": "k1"}, p.claims(), otra)
	if _, err := p.validador().Validar(context.Background(), token); !errors.Is(err, ErrInvalido) {
		t.Fatalf("deberia ser invalido, dio %v", err)
	}
}

func TestCuerpoAdulterado(t *testing.T) {
	p := nuevoPool(t)
	partes := strings.Split(p.token(t, nil), ".")
	otro := strings.Split(p.token(t, map[string]any{"sub": "otro-usuario"}), ".")
	adulterado := partes[0] + "." + otro[1] + "." + partes[2]
	if _, err := p.validador().Validar(context.Background(), adulterado); !errors.Is(err, ErrInvalido) {
		t.Fatalf("deberia ser invalido, dio %v", err)
	}
}

func TestAlgNoneYKidDesconocido(t *testing.T) {
	p := nuevoPool(t)
	v := p.validador()
	none := b64([]byte(`{"alg":"none","kid":"k1"}`)) + "." + strings.Split(p.token(t, nil), ".")[1] + "."
	if _, err := v.Validar(context.Background(), none); !errors.Is(err, ErrInvalido) {
		t.Fatalf("alg none deberia ser invalido, dio %v", err)
	}
	kid := p.firmar(t, map[string]any{"alg": "RS256", "kid": "k2"}, p.claims(), p.clave)
	if _, err := v.Validar(context.Background(), kid); !errors.Is(err, ErrInvalido) {
		t.Fatalf("kid desconocido deberia ser invalido, dio %v", err)
	}
}

func TestLasClavesSeBajanUnaVez(t *testing.T) {
	p := nuevoPool(t)
	v := p.validador()
	for range 3 {
		if _, err := v.Validar(context.Background(), p.token(t, nil)); err != nil {
			t.Fatal(err)
		}
	}
	kid := p.firmar(t, map[string]any{"alg": "RS256", "kid": "k2"}, p.claims(), p.clave)
	v.Validar(context.Background(), kid)
	if p.pedidos != 1 {
		t.Fatalf("el JWKS se bajo %d veces, esperado 1", p.pedidos)
	}
}

func TestJWKSCaidoNoEsTokenInvalido(t *testing.T) {
	p := nuevoPool(t)
	token := p.token(t, nil)
	p.servidor.Close()
	_, err := p.validador().Validar(context.Background(), token)
	if err == nil || errors.Is(err, ErrInvalido) {
		t.Fatalf("sin JWKS deberia fallar sin culpar al token, dio %v", err)
	}
}

func TestFormatoInvalido(t *testing.T) {
	p := nuevoPool(t)
	for _, token := range []string{"", "abc", "a.b", "a.b.c.d", "....", "a.b.c"} {
		if _, err := p.validador().Validar(context.Background(), token); err == nil {
			t.Fatalf("%q deberia ser invalido", token)
		}
	}
}

func TestSinConfiguracionNoValida(t *testing.T) {
	v := Nuevo("", "")
	if v.Habilitado() {
		t.Fatal("sin issuer ni client id no deberia estar habilitado")
	}
	if _, err := v.Validar(context.Background(), "lo.que.sea"); err == nil {
		t.Fatal("validar sin configuracion deberia fallar")
	}
}

func TestDelEncabezado(t *testing.T) {
	casos := map[string]string{
		"Bearer abc":  "abc",
		"bearer abc":  "abc",
		"BEARER  abc": "abc",
		"abc":         "",
		"Basic abc":   "",
		"Bearer":      "",
		"Bearer ":     "",
		"":            "",
	}
	for entrada, esperado := range casos {
		if dio := DelEncabezado(entrada); dio != esperado {
			t.Errorf("DelEncabezado(%q) = %q, esperado %q", entrada, dio, esperado)
		}
	}
}
