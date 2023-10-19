package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func GeneratePresignedUrl(key string, client *s3.Client) (string, error) {
	presignClient := s3.NewPresignClient(client)
	url, err := presignClient.PresignGetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String("toktik-videos"),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", err
	}
	return url.URL, nil
}

func GetS3Client(region string) (*s3.Client, error) {
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
		return nil, err
	}

	_, err = cfg.Credentials.Retrieve(context.TODO())
	if err != nil {
		return nil, err
	}

	return s3.NewFromConfig(cfg), nil
}

func GenerateVideoThumbnailUrl(client *s3.Client, username, videoKey string) (string, error) {
	thumbnailKey := fmt.Sprintf("users/%s/videos/%s/thumbnail", username, videoKey)
	return GeneratePresignedUrl(thumbnailKey, client)
}

// Creates a HLS file with presigned urls.
// Input:
// - username
// - videoKey
func GenerateHSLFile(client *s3.Client, username, videoKey string) (bytes.Buffer, error) {
	root := fmt.Sprintf("users/%s/videos/%s", username, videoKey)

	// Get the HLS root file.
	key := aws.String(fmt.Sprintf("%s/vid.m3u8", root))
	object, err := client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String("toktik-videos"),
		Key:    key,
	})
	if err != nil {
		log.Println("Failed to get object:", err)
		return bytes.Buffer{}, err
	}

	// Scan the content of the HLS file, line by line.
	scanner := bufio.NewScanner(object.Body)

	// Create a new buffer which will hold the generated file we'll send over.
	var buf bytes.Buffer

	for scanner.Scan() {
		scanned := scanner.Text()
		if strings.HasPrefix(scanned, "vid") {
			fileKey := fmt.Sprintf("%s/%s", root, scanned)
			url, err := GeneratePresignedUrl(fileKey, client)
			if err != nil {
				log.Println("Error", err)
				return bytes.Buffer{}, err
			}
			buf.WriteString(url)
		} else {
			buf.WriteString(scanned)
		}
		buf.WriteString("\n")
	}

	if err := scanner.Err(); err != nil {
		log.Println(err)
		return bytes.Buffer{}, err
	}

	return buf, nil
}
