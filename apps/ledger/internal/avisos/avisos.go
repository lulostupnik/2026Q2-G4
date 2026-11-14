package avisos

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
)

const atributoTitular = "titular"

type clienteSNS interface {
	Subscribe(ctx context.Context, in *sns.SubscribeInput, opts ...func(*sns.Options)) (*sns.SubscribeOutput, error)
	Publish(ctx context.Context, in *sns.PublishInput, opts ...func(*sns.Options)) (*sns.PublishOutput, error)
}

type Avisador struct {
	sns   clienteSNS
	topic string
}

func Nuevo(ctx context.Context, region, topic string) (*Avisador, error) {
	if topic == "" {
		return &Avisador{}, nil
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("cargar credenciales de AWS: %w", err)
	}
	return &Avisador{sns: sns.NewFromConfig(cfg), topic: topic}, nil
}

func (a *Avisador) Habilitado() bool { return a != nil && a.topic != "" }

func (a *Avisador) Topic() string { return a.topic }

func (a *Avisador) Suscribir(ctx context.Context, titular, email string) (string, error) {
	filtro, err := json.Marshal(map[string][]string{atributoTitular: {titular}})
	if err != nil {
		return "", err
	}
	salida, err := a.sns.Subscribe(ctx, &sns.SubscribeInput{
		TopicArn:              aws.String(a.topic),
		Protocol:              aws.String("email"),
		Endpoint:              aws.String(email),
		Attributes:            map[string]string{"FilterPolicy": string(filtro)},
		ReturnSubscriptionArn: true,
	})
	if err != nil {
		return "", fmt.Errorf("suscribir %s: %w", email, err)
	}
	return aws.ToString(salida.SubscriptionArn), nil
}

func (a *Avisador) Avisar(ctx context.Context, titular, asunto, mensaje string) error {
	_, err := a.sns.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(a.topic),
		Subject:  aws.String(asunto),
		Message:  aws.String(mensaje),
		MessageAttributes: map[string]types.MessageAttributeValue{
			atributoTitular: {DataType: aws.String("String"), StringValue: aws.String(titular)},
		},
	})
	if err != nil {
		return fmt.Errorf("avisar al titular: %w", err)
	}
	return nil
}
