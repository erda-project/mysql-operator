package v1

import "testing"

func TestMysqlDefaultImage(t *testing.T) {
	tests := []struct {
		name    string
		version string
		image   string
		want    string
	}{
		{
			name:    "mysql 8.0",
			version: "v8.0",
			want:    "registry.erda.cloud/erda-addons/mylet:8.0.40",
		},
		{
			name:    "mysql 5.7",
			version: "v5.7",
			want:    "registry.erda.cloud/erda-addons/mylet:v5.7",
		},
		{
			name:    "explicit image",
			version: "v8.0",
			image:   "registry.example.com/custom/mylet:latest",
			want:    "registry.example.com/custom/mylet:latest",
		},
		{
			name:    "legacy ERDA default image",
			version: "v8.0",
			image:   "registry.erda.cloud/erda/mylet:202509011647",
			want:    "registry.erda.cloud/erda-addons/mylet:8.0.40",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mysql := &Mysql{Spec: MysqlSpec{Version: tt.version, Image: tt.image}}
			mysql.Default()
			if mysql.Spec.Image != tt.want {
				t.Fatalf("image = %q, want %q", mysql.Spec.Image, tt.want)
			}
		})
	}
}
