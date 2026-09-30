// Cámara web: cualquier dispositivo con navegador se une como cámara del
// aeropuerto abriendo una página, sin instalar nada.
//
//	navegador ──WebSocket (JPEG)──▶ este servicio ──MJPEG──▶ camara_telefono.py (GPU)
//	    ▲                                 │  ▲                          │
//	    └──── sala: todas las cámaras ────┘  └── relevo del backend ◀───┘ (video + detecciones)
//
// Cada cámara se registra sola en Teléfonos (POST/DELETE /api/v1/telefonos).
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	api := env("API_VIVO", "http://backend-vivo:8080")
	camaras := NuevasCamaras(NuevaAPIAeropuerto(api), env("URL_MJPEG", "http://127.0.0.1:8092"), 10*time.Second)
	sala := NuevaSala(ctx, relevoDe(api), camaras)
	s := NuevoServidor(camaras, sala)
	home, _ := os.UserHomeDir()
	cert, err := certificado(env("CERT_DIR", filepath.Join(home, "certs")))
	if err != nil {
		log.Fatalf("certificado: %v", err)
	}

	// HTTPS para los dispositivos: el navegador solo da la cámara en contexto
	// seguro. Sin HTTP/2 (TLSNextProto vacío): el WebSocket necesita HTTP/1.1.
	publico := &http.Server{Addr: ":8443", Handler: s.Publico(), ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:    &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}}
	interno := &http.Server{Addr: ":8080", Handler: s.Interno(), ReadHeaderTimeout: 10 * time.Second}

	fallo := make(chan error, 2)
	go func() { fallo <- publico.ListenAndServeTLS("", "") }()
	go func() { fallo <- interno.ListenAndServe() }()
	// Cada 2 s: la lista de Teléfonos manda sobre quién está unido y qué muestra la sala.
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			c, cancel := context.WithTimeout(ctx, 2*time.Second)
			if lista, err := camaras.Sincronizar(c); err == nil {
				sala.Actualizar(lista)
			}
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	log.Printf("cámara web lista: dispositivos en https :8443, MJPEG para el modelo en :8080")

	select {
	case <-ctx.Done():
	case err := <-fallo:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("servidor: %v", err)
		}
	}
	// Nada queda registrado en Teléfonos después de apagar.
	camaras.RetirarTodas()
	apagar, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = publico.Shutdown(apagar)
	_ = interno.Shutdown(apagar)
}

func env(clave, porDefecto string) string {
	if v := os.Getenv(clave); v != "" {
		return v
	}
	return porDefecto
}

// certificado carga el autofirmado de dir o crea uno. Vive en el volumen
// camara-cert (nunca en el repo): sobrevive a reconstruir el contenedor, así
// los teléfonos no vuelven a ver la advertencia.
func certificado(dir string) (tls.Certificate, error) {
	archivoCrt, archivoClave := filepath.Join(dir, "camara-web.crt"), filepath.Join(dir, "camara-web.key")
	if c, err := tls.LoadX509KeyPair(archivoCrt, archivoClave); err == nil {
		if hoja, err := x509.ParseCertificate(c.Certificate[0]); err == nil && time.Now().Add(24*time.Hour).Before(hoja.NotAfter) {
			return c, nil
		}
	}
	clave, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serie, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return tls.Certificate{}, err
	}
	ahora := time.Now()
	plantilla := &x509.Certificate{
		SerialNumber: serie,
		Subject:      pkix.Name{CommonName: "camara-web"},
		NotBefore:    ahora.Add(-time.Hour),
		// iOS rechaza certificados de más de 825 días aunque se acepte la advertencia.
		NotAfter:    ahora.Add(800 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, plantilla, plantilla, &clave.PublicKey, clave)
	if err != nil {
		return tls.Certificate{}, err
	}
	claveDER, err := x509.MarshalECPrivateKey(clave)
	if err != nil {
		return tls.Certificate{}, err
	}
	crtPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	clavePEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: claveDER})
	if os.MkdirAll(dir, 0o700) == nil {
		_ = os.WriteFile(archivoCrt, crtPEM, 0o644)
		_ = os.WriteFile(archivoClave, clavePEM, 0o600)
	}
	return tls.X509KeyPair(crtPEM, clavePEM)
}
