package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", upload)
	log.Println("Server started successfully, listening on port 7000.")
	log.Fatal(http.ListenAndServe(":7000", nil))
}

func upload(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		svc := NewVideoUploadService()

		// Limit the size to 32mb.
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20+512)

		// Parse the file data.
		err := r.ParseMultipartForm(32 << 20) // 32Mb
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		// Get the file from the request.
		file, file_headers, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		defer file.Close()

		// Create the context for our service.
		ctx := context.Background()
		ctx = context.WithValue(ctx, "file", file)
		ctx = context.WithValue(ctx, "file_header", file_headers)

		// Perform the service.
		msg, err := svc.Action(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		// Let's just log, I don't know what is going on.
		fmt.Fprintf(w, "%v\n", msg)
	}
}
