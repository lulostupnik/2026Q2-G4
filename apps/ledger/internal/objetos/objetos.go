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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var ErrNoExiste = errors.New("el objeto no existe")

type Subido struct {
	URI   string
	Bytes int64
}

type Almacen interface {
	Guardar(ctx context.Context, clave string, cuerpo []byte, tipoContenido string) (string, error)
	Subir(ctx context.Context, clave, tipoContenido string, escribir func(io.Writer) error) (Subido, error)
	LeerRango(ctx context.Context, clave string, desde, hasta int64) ([]byte, error)
	URLDescarga(ctx context.Context, clave, nombre string, vigencia time.Duration) (string, error)
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
	cliente := s3.NewFromConfig(cfg)
	return &ese3{cliente: cliente, firmador: s3.NewPresignClient(cliente), bucket: bucket}, nil
}

type contador struct {
	w io.Writer
	n int64
}

func (c *contador) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

type ese3 struct {
	cliente  *s3.Client
	firmador *s3.PresignClient
	bucket   string
}

func (e *ese3) uri(clave string) string { return fmt.Sprintf("s3://%s/%s", e.bucket, clave) }

func (e *ese3) Guardar(ctx context.Context, clave string, cuerpo []byte, tipoContenido string) (string, error) {
	_, err := e.cliente.PutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(e.bucket),
		Key:               aws.String(clave),
		Body:              bytes.NewReader(cuerpo),
		ContentType:       aws.String(tipoContenido),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	})
	if err != nil {
		return "", fmt.Errorf("subir %s: %w", e.uri(clave), err)
	}
	return e.uri(clave), nil
}

func (e *ese3) Subir(ctx context.Context, clave, tipoContenido string, escribir func(io.Writer) error) (Subido, error) {
	lector, escritor := io.Pipe()
	cuenta := &contador{w: escritor}
	errEscritura := make(chan error, 1)
	go func() {
		err := escribir(cuenta)
		escritor.CloseWithError(err)
		errEscritura <- err
	}()

	subidor := manager.NewUploader(e.cliente, func(u *manager.Uploader) {
		u.PartSize = 16 << 20
		u.Concurrency = 2
	})
	_, err := subidor.Upload(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(e.bucket),
		Key:               aws.String(clave),
		Body:              lector,
		ContentType:       aws.String(tipoContenido),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	})
	lector.CloseWithError(err)
	errE := <-errEscritura
	switch {
	case err == nil && errE == nil:
		return Subido{URI: e.uri(clave), Bytes: cuenta.n}, nil
	case err == nil:
		return Subido{}, errE
	case errE == nil || errors.Is(err, errE):
		return Subido{}, fmt.Errorf("subir %s: %w", e.uri(clave), err)
	default:
		return Subido{}, fmt.Errorf("subir %s: %w", e.uri(clave), errors.Join(errE, err))
	}
}

func (e *ese3) LeerRango(ctx context.Context, clave string, desde, hasta int64) ([]byte, error) {
	if hasta <= desde {
		return nil, nil
	}
	salida, err := e.cliente.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(e.bucket),
		Key:    aws.String(clave),
		Range:  aws.String(fmt.Sprintf("bytes=%d-%d", desde, hasta-1)),
	})
	if err != nil {
		var faltante *types.NoSuchKey
		if errors.As(err, &faltante) {
			return nil, ErrNoExiste
		}
		return nil, fmt.Errorf("leer %s: %w", e.uri(clave), err)
	}
	defer salida.Body.Close()

	return io.ReadAll(salida.Body)
}

func (e *ese3) URLDescarga(ctx context.Context, clave, nombre string, vigencia time.Duration) (string, error) {
	firmado, err := e.firmador.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(e.bucket),
		Key:                        aws.String(clave),
		ResponseContentDisposition: aws.String(fmt.Sprintf("attachment; filename=%q", nombre)),
	}, s3.WithPresignExpires(vigencia))
	if err != nil {
		return "", fmt.Errorf("firmar la descarga de %s: %w", e.uri(clave), err)
	}
	return firmado.URL, nil
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

func (d *disco) Guardar(ctx context.Context, clave string, cuerpo []byte, tipoContenido string) (string, error) {
	subido, err := d.Subir(ctx, clave, tipoContenido, func(w io.Writer) error {
		_, err := w.Write(cuerpo)
		return err
	})
	return subido.URI, err
}

func (d *disco) Subir(_ context.Context, clave, _ string, escribir func(io.Writer) error) (Subido, error) {
	destino, err := d.ruta(clave)
	if err != nil {
		return Subido{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destino), 0o755); err != nil {
		return Subido{}, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(destino), ".tmp-*")
	if err != nil {
		return Subido{}, err
	}
	defer os.Remove(tmp.Name())

	cuenta := &contador{w: tmp}
	if err := escribir(cuenta); err != nil {
		tmp.Close()
		return Subido{}, err
	}
	if err := tmp.Close(); err != nil {
		return Subido{}, err
	}
	if err := os.Rename(tmp.Name(), destino); err != nil {
		return Subido{}, err
	}
	return Subido{URI: "file://" + destino, Bytes: cuenta.n}, nil
}

func (d *disco) LeerRango(_ context.Context, clave string, desde, hasta int64) ([]byte, error) {
	origen, err := d.ruta(clave)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(origen)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoExiste
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if hasta <= desde {
		return nil, nil
	}
	buf := make([]byte, hasta-desde)
	n, err := f.ReadAt(buf, desde)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:n], nil
}

func (d *disco) URLDescarga(_ context.Context, clave, _ string, _ time.Duration) (string, error) {
	origen, err := d.ruta(clave)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(origen); errors.Is(err, os.ErrNotExist) {
		return "", ErrNoExiste
	}
	return "file://" + origen, nil
}
