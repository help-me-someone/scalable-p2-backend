package main

import (
	"context"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Service interface {
	Action(client *s3.PresignClient) (string, error)
}

type VideoUploadService struct{}

func NewVideoUploadService() Service {
	return &VideoUploadService{}
}

// This service will simply return the presigned url for the upload.
func (v *VideoUploadService) Action(client *s3.PresignClient) (string, error) {
	expiryDate := time.Now().AddDate(0, 0, 1)
	presignExpiry := time.Hour * 1

	putObjectArgs := &s3.PutObjectInput{
		Bucket:  aws.String("toktik-videos"),
		Key:     aws.String("mykeywhat"),
		Expires: &expiryDate,
	}

	log.Println("Log: mykeywhat")

	res, err := client.PresignPutObject(
		context.Background(),
		putObjectArgs,
		s3.WithPresignExpires(presignExpiry),
	)

	if err != nil {
		return "", err
	}

	return res.URL, nil
}
