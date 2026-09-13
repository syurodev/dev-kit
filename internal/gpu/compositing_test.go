package gpu

import "testing"

func TestSoftwareCompositing(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"nothing set keeps the GPU path", map[string]string{}, false},
		{"DMABUF off, as configureWebviewEnv sets it", map[string]string{"WEBKIT_DISABLE_DMABUF_RENDERER": "1"}, true},
		{"an operator's explicit 0 keeps it on", map[string]string{"WEBKIT_DISABLE_DMABUF_RENDERER": "0"}, false},
		{"an empty value still counts as set, as in WebKit", map[string]string{"WEBKIT_DISABLE_DMABUF_RENDERER": ""}, true},
		{"compositing mode off", map[string]string{"WEBKIT_DISABLE_COMPOSITING_MODE": "1"}, true},
		{"compositing 0 with DMABUF on", map[string]string{"WEBKIT_DISABLE_COMPOSITING_MODE": "0", "WEBKIT_DISABLE_DMABUF_RENDERER": "0"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				value, ok := c.env[key]
				return value, ok
			}
			if got := SoftwareCompositing(lookup); got != c.want {
				t.Fatalf("SoftwareCompositing(%v) = %v, want %v", c.env, got, c.want)
			}
		})
	}
}
