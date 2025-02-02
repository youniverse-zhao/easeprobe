/*
 * Copyright (c) 2022, MegaEase
 * All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/megaease/easeprobe/global"
)

var ca *x509.Certificate = &x509.Certificate{
	Subject: pkix.Name{
		Organization: []string{"FAKE EASE PROBE"},
	},
	SerialNumber:          big.NewInt(1),
	NotBefore:             time.Now().Add(time.Hour * 24 * -365),
	NotAfter:              time.Now().Add(time.Hour * 24 * 365),
	IsCA:                  true,
	ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	BasicConstraintsValid: true,
}

var capriv *rsa.PrivateKey
var cabytes []byte

func createCert(template *x509.Certificate) (*tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca, &priv.PublicKey, capriv)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}, nil
}

func init() {
	global.InitEaseProbe("easeprobe", "http://404")

	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		panic(fmt.Errorf("failed to generate private key: %v", err))
	}

	capriv = priv

	data, err := x509.CreateCertificate(rand.Reader, ca, ca, &priv.PublicKey, priv)
	if err != nil {
		panic(fmt.Errorf("failed to create certificate: %v", err))
	}

	cabytes = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: data,
	})
}

type mockServer struct {
	server   net.Listener
	hostname string
}

func newTlsMockServer(template *x509.Certificate) (*mockServer, error) {
	cert, err := createCert(template)
	if err != nil {
		return nil, err
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}

	s, err := tls.Listen("tcp", "0.0.0.0:0", config)
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			c, err := s.Accept()
			if err != nil {
				return
			}
			c.Write([]byte("hello")) // force tls handshake
		}
	}()

	m := &mockServer{
		server:   s,
		hostname: s.Addr().String(),
	}

	return m, nil
}

func (m *mockServer) Close() error {
	return m.server.Close()
}

func TestTlsSimple(t *testing.T) {
	mock, err := newTlsMockServer(&x509.Certificate{
		DNSNames:              []string{"0.0.0.0"},
		IPAddresses:           []net.IP{net.IPv4zero, net.IPv6loopback, net.IPv6unspecified},
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(time.Hour * 24 * -365),
		NotAfter:              time.Now().Add(time.Hour * 24 * 365),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	})
	if err != nil {
		t.Errorf("newTlsMockServer error: %v", err)
		return
	}
	defer mock.Close()

	tls := &TLS{
		Host:      mock.hostname,
		RootCaPem: string(cabytes),
	}

	tls.Config(global.ProbeSettings{
		Timeout: time.Second * 10,
	})

	ok, msg := tls.DoProbe()
	if !ok {
		t.Errorf("tls probe failed %v", msg)
	}
}

func TestTlsUntrust(t *testing.T) {
	mock, err := newTlsMockServer(&x509.Certificate{
		DNSNames:              []string{"0.0.0.0"},
		IPAddresses:           []net.IP{net.IPv4zero, net.IPv6loopback, net.IPv6unspecified},
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(time.Hour * 24 * -365),
		NotAfter:              time.Now().Add(time.Hour * 24 * 365),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	})
	if err != nil {
		t.Errorf("newTlsMockServer error: %v", err)
		return
	}
	defer mock.Close()

	t.Run("no root ca", func(t *testing.T) {
		tls := &TLS{
			Host: mock.hostname,
		}

		tls.Config(global.ProbeSettings{
			Timeout: time.Second * 10,
		})

		ok, _ := tls.DoProbe()
		if ok {
			t.Error("tls probe should fail for untrust root")
		}
	})

	t.Run("ignore root ca", func(t *testing.T) {
		tls := &TLS{
			Host:               mock.hostname,
			InsecureSkipVerify: true,
		}

		tls.Config(global.ProbeSettings{
			Timeout: time.Second * 10,
		})

		ok, msg := tls.DoProbe()
		if !ok {
			t.Errorf("tls probe failed %v", msg)
		}
	})
}

func TestTlsExpired(t *testing.T) {
	mock, err := newTlsMockServer(&x509.Certificate{
		DNSNames:              []string{"0.0.0.0"},
		IPAddresses:           []net.IP{net.IPv4zero, net.IPv6loopback, net.IPv6unspecified},
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(time.Hour * 24 * -366),
		NotAfter:              time.Now().Add(time.Hour * 24 * -365),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	})
	if err != nil {
		t.Errorf("newTlsMockServer error: %v", err)
		return
	}
	defer mock.Close()

	t.Run("expired", func(t *testing.T) {
		tls := &TLS{
			Host:               mock.hostname,
			InsecureSkipVerify: true,
		}

		tls.Config(global.ProbeSettings{
			Timeout: time.Second * 10,
		})

		ok, msg := tls.DoProbe()
		if ok {
			t.Error("tls probe should fail for expired")
		}

		if msg != "certificate is expired or not yet valid" {
			t.Error("tls probe should fail return wrong expired msg")
		}
	})

	t.Run("ignore expired", func(t *testing.T) {
		tls := &TLS{
			Host:               mock.hostname,
			InsecureSkipVerify: true,
			ExpireSkipVerify:   true,
		}

		tls.Config(global.ProbeSettings{
			Timeout: time.Second * 10,
		})

		ok, msg := tls.DoProbe()
		if !ok {
			t.Errorf("tls probe failed %v", msg)
		}
	})
}
