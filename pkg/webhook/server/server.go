package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	tlsDir      = `/run/secrets/tls`
	tlsCertFile = `tls.crt`
	tlsKeyFile  = `tls.key`
)

// StartServer starts the server
func StartServer() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8443"
	}

	var mixSchedulerRequierd bool

	if val := os.Getenv("mixSchedulerRequierd"); val != "" {
		mixSchedulerRequierd = val == "true"
	}

	var notControllerNamespace map[string]struct{}
	if val := os.Getenv("notControllerNamespace"); val != "" {

		notControllerNamespace = make(map[string]struct{})
		for _, ns := range strings.Split(strings.TrimSpace(val), ",") {
			notControllerNamespace[ns] = struct{}{}
		}
	} else {
		// default notControllerNamespace
		notControllerNamespace = map[string]struct{}{
			"kube-system":          {},
			"mix-scheduler-system": {},
		}
	}

	app := &App{
		mixSchedulerRequierd:   mixSchedulerRequierd,
		notControllerNamespace: notControllerNamespace,
	}

	mux := BuildRouter(app)

	fmt.Printf("Listening on port %s\n", port)

	certPath := filepath.Join(tlsDir, tlsCertFile)
	keyPath := filepath.Join(tlsDir, tlsKeyFile)
	server := &http.Server{
		// We listen on port 8443 such that we do not need root privileges or extra capabilities for this server.
		// The Service object will take care of mapping this port to the HTTPS port 443.
		Addr:    ":" + port,
		Handler: mux,
	}
	return server.ListenAndServeTLS(certPath, keyPath)
}
