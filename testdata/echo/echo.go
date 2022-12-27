package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	var (
		port int
		name string
	)

	flag.StringVar(&name, "name", strconv.Itoa(int(time.Now().Unix())), "server name")
	flag.IntVar(&port, "port", 3999, "listening port")
	flag.Parse()

	echoServer := http.Server{Addr: fmt.Sprintf("localhost:%d", port)}

	startTime := time.Now()
	http.DefaultServeMux.HandleFunc("/shutdown", func(_ http.ResponseWriter, _ *http.Request) {
		// gracefully shutdown the server
		_ = echoServer.Shutdown(context.TODO())
	})
	http.DefaultServeMux.HandleFunc("/die", func(_ http.ResponseWriter, _ *http.Request) {
		// exit with status code 2
		os.Exit(2)
	})
	http.DefaultServeMux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("content-type", r.Header.Get("content-type"))
		rw.Header().Set("x-server-name", name)
		rw.Header().Set("x-request-path", r.RequestURI)
		rw.Header().Set("x-server-start-time", startTime.Format("2006-01-02 15:04:05.999999"))
		rw.Header().Set("x-server-current-time", time.Now().Format("2006-01-02 15:04:05.999999"))
		rw.WriteHeader(http.StatusOK)
		buf := make([]byte, 10240)
		if n, err := r.Body.Read(buf); err != nil {
			_, _ = rw.Write(buf[:n])
		}
	})
	_ = echoServer.ListenAndServe()
}
