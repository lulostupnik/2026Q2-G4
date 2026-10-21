package objetos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var ErrNoExiste = errors.New("el objeto no existe")

type Almacen interface {
	Guardar(ctx context.Context, clave string, cuerpo []byte, tipoContenido string) (string, error)
	Leer(ctx context.Context, clave string) ([]byte, error)
}

func Desde(ctx context.Context, region, bucket, dir string) (Almacen, error) {
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("crear %s: %w", dir, err)
		}
		return &disco{raiz: dir}, nil
	}
	if bucket == "" {
		return nil, errors.New("falta S3_BUCKET (o OBJETOS_DIR para correr local)")
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("cargar credenciales de AWS: %w", err)
	}
	return &ese3{cliente: s3.NewFromConfig(cfg), bucket: bucket}, nil
}

type ese3 struct {
	cliente *s3.Client
	bucket  string
}

func (e *ese3) Guardar(ctx context.Context, clave string, cuerpo []byte, tipoContenido string) (string, error) {
	_, err := e.cliente.PutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(e.bucket),
		Key:               aws.String(clave),
		Body:              bytes.NewReader(cuerpo),
		ContentType:       aws.String(tipoContenido),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	})
	if err != nil {
		return "", fmt.Errorf("subir s3://%s/%s: %w", e.bucket, clave, err)
	}
	return fmt.Sprintf("s3://%s/%s", e.bucket, clave), nil
}

func (e *ese3) Leer(ctx context.Context, clave string) ([]byte, error) {
	salida, err := e.cliente.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(e.bucket),
		Key:    aws.String(clave),
	})
	if err != nil {
		var faltante *types.NoSuchKey
		if errors.As(err, &faltante) {
			return nil, ErrNoExiste
		}
		return nil, fmt.Errorf("leer s3://%s/%s: %w", e.bucket, clave, err)
	}
	defer salida.Body.Close()

	return io.ReadAll(salida.Body)
}

type disco struct{ raiz string }

func (d *disco) ruta(clave string) (string, error) {
	limpia := filepath.Clean("/" + filepath.FromSlash(clave))
	completa := filepath.Join(d.raiz, limpia)
	if !strings.HasPrefix(completa, filepath.Clean(d.raiz)+string(os.PathSeparator)) {
		return "", fmt.Errorf("clave invalida: %s", clave)
	}
	return completa, nil
}

func (d *disco) Guardar(_ context.Context, clave string, cuerpo []byte, _ string) (string, error) {
	destino, err := d.ruta(clave)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(destino), 0o755); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(filepath.Dir(destino), ".tmp-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(cuerpo); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), destino); err != nil {
		return "", err
	}
	return "file://" + destino, nil
}

func (d *disco) Leer(_ context.Context, clave string) ([]byte, error) {
	origen, err := d.ruta(clave)
	if err != nil {
		return nil, err
	}
	cuerpo, err := os.ReadFile(origen)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoExiste
	}
	return cuerpo, err
}
