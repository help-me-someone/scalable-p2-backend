package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// gorm.DB objects are meant to be reused.
var GORM_CONNECTION_SINGLETON *gorm.DB

// Timer function from https://stackoverflow.com/questions/45766572/is-there-an-efficient-way-to-calculate-execution-time-in-golang
func timer(name string) func() {
	start := time.Now()
	return func() {
		fmt.Printf("%s took %v\n", name, time.Since(start))
	}
}

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

	scanner := bufio.NewScanner(object.Body)
	urlMappings := make(map[int]string)

	// Create a wait group
	var wg sync.WaitGroup
	count := 0
	for scanner.Scan() {
		scanned := scanner.Text()
		wg.Add(1)
		go func(scanned string, line int) {
			defer wg.Done()
			if strings.HasPrefix(scanned, "vid") {
				fileKey := fmt.Sprintf("%s/%s", root, scanned)
				url, _ := GeneratePresignedUrl(fileKey, client)
				urlMappings[line] = fmt.Sprintf("%s\n", url)
			} else {
				urlMappings[line] = fmt.Sprintf("%s\n", scanned)
			}
		}(scanned, count)
		count += 1
	}
	wg.Wait()

	log.Println("Waiting...")

	var answerBuf bytes.Buffer

	for v := 0; v < count; v++ {
		line, _ := urlMappings[v]
		answerBuf.WriteString(line)
	}

	if err := scanner.Err(); err != nil {
		log.Println(err)
		return bytes.Buffer{}, err
	}

	return answerBuf, nil
}

// Create and return a new database connection. Ideally we
// would check the logins etc, but we don't live in an ideal
// world here. I got deadlines to meet. Well technically if
// the credentials are wrong the connection could never be
// made, so I guess it's fine...?
func GetDatabaseConnection(username, password, server string) (*gorm.DB, error) {
	if GORM_CONNECTION_SINGLETON == nil {
		dsn := fmt.Sprintf(
			"%s:%s@tcp(%s)/toktik-db?charset=utf8mb4&parseTime=True&loc=Local",
			username,
			password,
			server,
		)
		connection, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err != nil {
			return nil, err
		} else {
			GORM_CONNECTION_SINGLETON = connection
		}
	}
	return GORM_CONNECTION_SINGLETON, nil
}
