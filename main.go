package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
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

type S3PresignGetObjectAPI interface {
	PresignGetObject(
		ctx context.Context,
		params *s3.GetObjectInput,
		optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

func GetPresignedURL(c context.Context, api S3PresignGetObjectAPI, input *s3.GetObjectInput) (*v4.PresignedHTTPRequest, error) {
	return api.PresignGetObject(c, input)
}

func upload(w http.ResponseWriter, r *http.Request) {
	// if r.Method == "POST" || r.Method == "OPTIONS" {

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

	resp, err := presignClient.PresignPutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String("toktik-videos"),
		Key:    aws.String("cat.png"),
	})

	if err != nil {
		fmt.Println("Got an error retrieving pre-signed object:")
		fmt.Println(err)
		return
	}

	fmt.Println("The URL: ")
	fmt.Println(resp.URL)
	// svc := NewVideoUploadService()

	// // Perform the service.
	// msg, err := svc.Action(presignClient)

	// if err != nil {
	// 	log.Println("Action failed.")
	// 	log.Println(err.Error())
	// 	http.Error(w, err.Error(), http.StatusBadRequest)

	// 	resp := map[string]interface{}{
	// 		"success": false,
	// 		"message": err.Error(),
	// 	}
	// 	json.NewEncoder(w).Encode(resp)
	// 	return
	// }

	// resp := map[string]interface{}{
	// 	"success": true,
	// 	"message": msg,
	// }
	// json.NewEncoder(w).Encode(resp)

	// Let's just log, I don't know what is going on.
	// log.Println(msg)
	// return
	// }
	// log.Println("Incorrect method type.")

	// w.WriteHeader(http.StatusBadRequest)
	// resp := map[string]interface{}{
	// 	"success": false,
	// 	"message": "incorrect method type",
	// }
	// json.NewEncoder(w).Encode(resp)
}
