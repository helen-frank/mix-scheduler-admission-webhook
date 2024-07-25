package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

const (
	tlsDir      = `/run/secrets/tls`
	tlsCertFile = `tls.crt`
	tlsKeyFile  = `tls.key`
)

// env
// PORT, mixSchedulerRequierd, notControllerNamespace, SPOT_NODE_WEIGHT, ONDEMAND_NODE_WEIGHT

// StartServer starts the server
func StartServer() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8443"
	}

	// Enabled mix-scheduler
	var mixSchedulerRequierd = true

	if val := os.Getenv("mixSchedulerRequierd"); val != "" {
		mixSchedulerRequierd = val == "true"
	}

	// notControllerNamespace
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

	onDemandMinPodNum := 1

	if val := os.Getenv("OnDemandMinPodNum"); val != "" {
		num, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		onDemandMinPodNum = num
	}

	spotMinPodNum := 1

	if val := os.Getenv("SpotMinPodNum"); val != "" {
		num, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		spotMinPodNum = num
	}

	app, err := NewDefaultApp(context.Background())
	if err != nil {
		return err
	}

	app.mixSchedulerRequierd = mixSchedulerRequierd
	app.notControllerNamespace = notControllerNamespace
	app.OnDemandMinPodNum = onDemandMinPodNum
	app.SpotMinPodNum = spotMinPodNum

	klog.Infof("OnDemandMinPodNum %v", app.OnDemandMinPodNum)
	klog.Infof("SpotMinPodNum %v", app.SpotMinPodNum)

	app.StartInformer()
	defer app.StopInformer()

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
