package api

import (
	_ "embed"
	"net/http"
)

// Contact links (identity.Contact.Link) point at /add on the default relay:
// https://<relay>/add#<key>?n=<name>. Messengers make https links clickable;
// on Android the app opens them itself (App Links, verified through
// /.well-known/assetlinks.json), elsewhere this page offers "Open in Mava",
// the download and the link to paste by hand. The contact is in the
// fragment, which browsers never send, so the relay doesn't learn it.

//go:embed add.html
var addPage []byte

// AndroidApp is the app allowed to open this relay's /add links directly.
const AndroidApp = "org.meshmsg.app"

// AndroidCerts are the SHA-256 fingerprints of the APK signing certificates
// (android/debug.keystore, used for release APKs until roadmap A1).
var AndroidCerts = []string{"46:72:0B:FA:36:DD:DD:7D:6C:A8:8A:F3:B0:2F:E8:B7:EA:48:06:37:A4:29:53:33:E0:2B:ED:6A:F9:21:E2:BC"}

func (s *Server) addLink(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Cache-Control", "public, max-age=3600")
	w.Write(addPage)
}

func (s *Server) assetLinks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, []map[string]any{{
		"relation": []string{"delegate_permission/common.handle_all_urls"},
		"target": map[string]any{
			"namespace":                "android_app",
			"package_name":             AndroidApp,
			"sha256_cert_fingerprints": AndroidCerts,
		},
	}})
}
