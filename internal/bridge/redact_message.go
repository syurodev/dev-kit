package bridge

import (
	"regexp"
	"strings"
)

/*
The driver's own sentence, minus the secrets it may be carrying.

"Connection failed." told the reader nothing: not whether the host refused,
the password was wrong, or the TLS handshake broke. The driver's message says
exactly that, but connecting happens with a DSN in hand, and some drivers echo
what they were given. So the message passes through with the parts that can
hold a secret masked: the password in a URL's userinfo, and the value of any
key that names a secret (password=…, pwd: …, token=…).
*/

const redactedSecret = "***"

var (
	// scheme://user:password@host → scheme://user:***@host
	urlPasswordPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/?#@\s]*):[^@/\s]*@`)
	// password=hunter2, pwd: hunter2, "token":"abc", sasl.password='x y'
	secretPairPattern = regexp.MustCompile(`(?i)([\w.-]*(?:password|passwd|passphrase|pwd|secret|token|apikey|api_key|credential|authorization|privatekey|private_key)[\w.-]*"?\s*[=:]\s*)("[^"]*"|'[^']*'|[^\s,;&)"']+)`)
)

func redactSecrets(message string) string {
	message = urlPasswordPattern.ReplaceAllString(message, "${1}:"+redactedSecret+"@")
	message = secretPairPattern.ReplaceAllString(message, "${1}"+redactedSecret)
	return strings.TrimSpace(message)
}
