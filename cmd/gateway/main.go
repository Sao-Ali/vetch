package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
	"github.com/Sao-Ali/vetch/internal/agent"
	"github.com/Sao-Ali/vetch/internal/gateway"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var address, namespace string
	flag.StringVar(&address, "listen-address", ":8082", "Inference listener")
	flag.StringVar(&namespace, "namespace", "default", "Namespace containing inference clusters")
	flag.Parse()
	if namespace == "" {
		return fmt.Errorf("namespace must not be empty")
	}
	scheme := runtime.NewScheme()
	if err := infrastructurev1alpha1.AddToScheme(scheme); err != nil {
		return err
	}
	config, err := ctrl.GetConfig()
	if err != nil {
		return err
	}
	reader, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	service := &gateway.Gateway{Client: reader, Namespace: namespace}
	return agent.Serve(ctx, listener, service.Handler())
}
