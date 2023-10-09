package main

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type Service interface {
	Action(context.Context) (string, error)
}

type VideoUploadService struct{}

func NewVideoUploadService() Service {
	return &VideoUploadService{}
}

// This service will simply get the file information and
// upload it to the S3 bucket on digital ocean.
func (v *VideoUploadService) Action(ctx context.Context) (string, error) {
	endpoint := "sgp1.digitaloceanspaces.com"
	region := "sgp1"
	myBucket := "toktik-videos"

	// Get multipart file.
	f := ctx.Value("file")
	if f == nil {
		return "", fmt.Errorf("File body not found.")
	}
	file, ok := f.(multipart.File)
	if !ok {
		return "", fmt.Errorf("Invalid file type. Could not covert to multipart file.")
	}

	// Get multipart header.
	mph := ctx.Value("file_header")
	if mph == nil {
		return "", fmt.Errorf("Multipart header not given.")
	}
	file_header, ok := mph.(*multipart.FileHeader)
	if !ok {
		return "", fmt.Errorf("Invalid multipart header type. Could not covert to multipart header.")
	}

	file_name := file_header.Filename
	file_size := file_header.Size
	file_buffer := make([]byte, file_size)
	file.Read(file_buffer)

	sess := session.Must(session.NewSession(&aws.Config{
		Endpoint: &endpoint,
		Region:   &region,
	}))

	// Create an uploader with the session and default options
	uploader := s3manager.NewUploader(sess)

	result, err := uploader.Upload(&s3manager.UploadInput{
		Bucket:      aws.String(myBucket),
		Key:         aws.String(file_name),
		ContentType: aws.String(http.DetectContentType(file_buffer)),
		Body:        bytes.NewReader(file_buffer),
	})

	if err != nil {
		return "", err
	}

	return aws.StringValue(&result.Location), nil

}
