package main

import (
	"net/http"
	"os"

	"Orpbal/internal/api"
)

func main() {
	addr := os.Getenv("OPENAPI_ADDR")
	if addr == "" {
		addr = ":8081"
	}
	if err := http.ListenAndServe(addr, api.NewOpenAPIHandler()); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
