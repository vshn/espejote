package plugins

import "os"

// Lifted from https://go.dev/src/crypto/x509/root_unix.go

const (
	// certFileEnv is the environment variable which identifies where to locate
	// the SSL certificate file. If set this overrides the system default.
	CertFileEnv = "SSL_CERT_FILE"

	// certDirEnv is the environment variable which identifies which directory
	// to check for SSL certificate files. If set this overrides the system default.
	// It is a colon separated list of directories.
	// See https://www.openssl.org/docs/man1.0.2/man1/c_rehash.html.
	CertDirEnv = "SSL_CERT_DIR"
)

// Lifted from https://go.dev/src/crypto/x509/root_linux.go
// Works on Linux and macOS with Homebrew.
// Used to inject the locations of CA certificates into the WASI environment

// Possible certificate files; stop after finding one.
var CertFiles = []string{
	"/etc/ssl/certs/ca-certificates.crt",                // Debian/Ubuntu/Gentoo etc.
	"/etc/pki/tls/certs/ca-bundle.crt",                  // Fedora/RHEL 6
	"/etc/ssl/ca-bundle.pem",                            // OpenSUSE
	"/etc/pki/tls/cacert.pem",                           // OpenELEC
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", // CentOS/RHEL 7
	"/etc/ssl/cert.pem",                                 // Alpine Linux
}

// Possible directories with certificate files; all will be read.
var CertDirectories = []string{
	"/etc/ssl/certs",     // SLES10/SLES11, https://golang.org/issue/12139
	"/etc/pki/tls/certs", // Fedora/RHEL
}

// FindFirstCertFile returns the first found certificate file from the list of
// possible certificate files. If no file is found, it returns an empty string.
// The second return value indicates whether a file was found.
func FindFirstCertFile() (string, bool) {
	for _, file := range CertFiles {
		if _, err := os.Stat(file); err == nil {
			return file, true
		}
	}
	return "", false
}
