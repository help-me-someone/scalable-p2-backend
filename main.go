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

func enableCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
}

func upload(w http.ResponseWriter, r *http.Request) {
	enableCors(&w)
	if r.Method == "POST" {
		svc := NewVideoUploadService()

		// Limit the size to 32mb.
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20+512)

		// Parse the file data.
		err := r.ParseMultipartForm(32 << 20) // 32Mb
		if err != nil {
			log.Println(err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		// Get the file from the request.
		file, file_headers, err := r.FormFile("file")
		if err != nil {
			log.Println(err.Error())
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
			log.Println(err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		// Let's just log, I don't know what is going on.
		log.Println(msg)
		fmt.Fprintf(w, "%v\n", msg)
	}
}
