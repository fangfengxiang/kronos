// Package etcdconfig opens the registry used by the Order reference commands.
package etcdconfig

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"github.com/fangfengxiang/kronos/infra/etcd"
)

// Open uses the deployment's optional CA and client certificate files.
func Open(endpoint string) (*etcd.Registry, error) {
	caFile := os.Getenv("KRONOS_ETCD_CA_FILE")
	certFile := os.Getenv("KRONOS_ETCD_CERT_FILE")
	keyFile := os.Getenv("KRONOS_ETCD_KEY_FILE")
	if caFile == "" && certFile == "" && keyFile == "" {
		return etcd.New(endpoint)
	}
	if (certFile == "") != (keyFile == "") {
		return nil, errors.New("set both KRONOS_ETCD_CERT_FILE and KRONOS_ETCD_KEY_FILE")
	}
	config := &tls.Config{}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read etcd CA file: %w", err)
		}
		config.RootCAs = x509.NewCertPool()
		if !config.RootCAs.AppendCertsFromPEM(pem) {
			return nil, errors.New("etcd CA file contains no trusted certificates")
		}
	}
	if certFile != "" {
		keyPair, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load etcd client certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{keyPair}
	}
	return etcd.NewWithConfig(etcd.Config{Endpoints: []string{endpoint}, TLS: config})
}
