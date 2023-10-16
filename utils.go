package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
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

// Creates a HLS file with presigned urls.
// Input:
// - root: The root file (the .m3u8)
func GenerateHSLFile(root string, client *s3.Client) (bytes.Buffer, error) {

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
