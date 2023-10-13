package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	region = "sgp1"
)

func main() {
	http.HandleFunc("/", upload)
	log.Println("Server started successfully, listening on port 7000.")
	log.Fatal(http.ListenAndServe(":7000", nil))
}

func upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "invalid method",
		}
		json.NewEncoder(w).Encode(resp)
	}

	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL: "https://" + region + ".digitaloceanspaces.com",
		}, nil
	})

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithEndpointResolverWithOptions(customResolver),
	)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		resp := map[string]interface{}{
			"success": false,
			"message": "user not specified",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Create the client.
	client := s3.NewFromConfig(cfg)

	// Presign the client.
	presignClient := s3.NewPresignClient(client)

	response, err := presignClient.PresignPutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String("toktik-videos"),
		Key:    aws.String("cat.png"),
	})

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		resp := map[string]interface{}{
			"success": false,
			"message": "error retrieving pre-signed object",
		}
		json.NewEncoder(w).Encode(resp)
		return
	}

	fmt.Println("The URL: ", response.URL)
	resp := map[string]interface{}{
		"success": true,
		"url":     response.URL,
	}
	json.NewEncoder(w).Encode(resp)
}
