package avisos

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type snsFalso struct {
	suscripciones []*sns.SubscribeInput
	publicaciones []*sns.PublishInput
}

func (f *snsFalso) Subscribe(_ context.Context, in *sns.SubscribeInput, _ ...func(*sns.Options)) (*sns.SubscribeOutput, error) {
	f.suscripciones = append(f.suscripciones, in)
	return &sns.SubscribeOutput{SubscriptionArn: aws.String("pending confirmation")}, nil
}

func (f *snsFalso) Publish(_ context.Context, in *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	f.publicaciones = append(f.publicaciones, in)
	return &sns.PublishOutput{}, nil
}

func TestCadaTitularRecibeSoloLoSuyo(t *testing.T) {
	falso := &snsFalso{}
	a := &Avisador{sns: falso, topic: "arn:aws:sns:us-east-1:123:ledger-titulares"}
	ctx := context.Background()

	if _, err := a.Suscribir(ctx, "sub-ana", "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	s := falso.suscripciones[0]
	if aws.ToString(s.Protocol) != "email" || aws.ToString(s.Endpoint) != "ana@example.com" {
		t.Fatalf("suscripcion inesperada: %+v", s)
	}
	var filtro map[string][]string
	if err := json.Unmarshal([]byte(s.Attributes["FilterPolicy"]), &filtro); err != nil {
		t.Fatalf("filter policy ilegible: %v", err)
	}
	if len(filtro) != 1 || len(filtro["titular"]) != 1 || filtro["titular"][0] != "sub-ana" {
		t.Fatalf("la suscripcion tiene que filtrar por el titular, tiene %v", filtro)
	}

	if err := a.Avisar(ctx, "sub-ana", "Tu exportación está lista", "texto"); err != nil {
		t.Fatal(err)
	}
	p := falso.publicaciones[0]
	if got := aws.ToString(p.MessageAttributes["titular"].StringValue); got != "sub-ana" {
		t.Fatalf("el aviso tiene que llevar el titular como atributo, lleva %q", got)
	}
}

func TestSinTopicNoHayAvisos(t *testing.T) {
	a, err := Nuevo(context.Background(), "us-east-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Habilitado() {
		t.Fatal("sin topic no tiene que estar habilitado")
	}
	var nulo *Avisador
	if nulo.Habilitado() {
		t.Fatal("un avisador nulo no tiene que estar habilitado")
	}
}
