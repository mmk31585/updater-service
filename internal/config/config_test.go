package config

import (
	"os"
	"testing"
	"time"
)

func TestGetString(t *testing.T) {
	t.Setenv("TEST_STRING_KEY", "env_value")
	os.Setenv("TEST_STRING_KEY", "env_value")
	defer os.Unsetenv("TEST_STRING_KEY")

	tests := []struct {
		name     string
		key      string
		fallback string
		want     string
	}{
		{"env_set", "TEST_STRING_KEY", "fallback", "env_value"},
		{"env_unset", "NONEXISTENT_KEY", "fallback", "fallback"},
		{"empty_env", "TEST_EMPTY", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetString(tt.key, tt.fallback)
			if got != tt.want {
				t.Errorf("GetString(%q, %q) = %q, want %q", tt.key, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestGetDuration(t *testing.T) {
	t.Setenv("TEST_DURATION_KEY", "5s")
	defer os.Unsetenv("TEST_DURATION_KEY")

	tests := []struct {
		name     string
		key      string
		fallback time.Duration
		want     time.Duration
	}{
		{"valid_duration", "TEST_DURATION_KEY", time.Second, 5 * time.Second},
		{"env_unset", "NONEXISTENT_KEY", time.Minute, time.Minute},
		{"invalid_duration", "TEST_INVALID_DURATION", time.Second, time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetDuration(tt.key, tt.fallback)
			if got != tt.fallback && tt.name == "env_unset" {
				if got != tt.want {
					t.Errorf("GetDuration(%q, %v) = %v, want %v", tt.key, tt.fallback, got, tt.want)
				}
			} else if tt.name == "valid_duration" && got != tt.want {
				t.Errorf("GetDuration(%q, %v) = %v, want %v", tt.key, tt.fallback, got, tt.want)
			}
		})
	}

	os.Setenv("TEST_INVALID_DURATION", "not_a_duration")
	got := GetDuration("TEST_INVALID_DURATION", time.Second)
	if got != time.Second {
		t.Errorf("GetDuration with invalid value returned %v, want fallback %v", got, time.Second)
	}
}

func TestGetInt(t *testing.T) {
	t.Setenv("TEST_INT_KEY", "42")
	defer os.Unsetenv("TEST_INT_KEY")

	tests := []struct {
		name     string
		key      string
		fallback int
		want     int
	}{
		{"valid_int", "TEST_INT_KEY", 0, 42},
		{"env_unset", "NONEXISTENT_KEY", 100, 100},
		{"invalid_int", "TEST_INVALID_INT", 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetInt(tt.key, tt.fallback)
			if got != tt.want {
				t.Errorf("GetInt(%q, %d) = %d, want %d", tt.key, tt.fallback, got, tt.want)
			}
		})
	}

	os.Setenv("TEST_INVALID_INT", "not_an_int")
	got := GetInt("TEST_INVALID_INT", 5)
	if got != 5 {
		t.Errorf("GetInt with invalid value returned %d, want fallback 5", got)
	}
}

func TestGetBool(t *testing.T) {
	t.Setenv("TEST_BOOL_KEY", "true")
	defer os.Unsetenv("TEST_BOOL_KEY")

	tests := []struct {
		name     string
		key      string
		fallback bool
		want     bool
	}{
		{"true", "TEST_BOOL_KEY", false, true},
		{"false", "TEST_BOOL_FALSE", false, false},
		{"env_unset", "NONEXISTENT_BOOL", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetBool(tt.key, tt.fallback)
			if got != tt.want {
				t.Errorf("GetBool(%q, %v) = %v, want %v", tt.key, tt.fallback, got, tt.want)
			}
		})
	}

	os.Setenv("TEST_BOOL_FALSE", "false")
	got := GetBool("TEST_BOOL_FALSE", true)
	if got != false {
		t.Errorf("GetBool('false') returned %v, want false", got)
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want int64
	}{
		{"empty", "", 1 << 20},
		{"KiB", "1KiB", 1024},
		{"MiB", "1MiB", 1024 * 1024},
		{"GiB", "1GiB", 1024 * 1024 * 1024},
		{"plain_bytes", "1024", 1024},
		{"large_bytes", "1048576", 1048576},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSize(tt.s)
			if got != tt.want {
				t.Errorf("ParseSize(%q) = %d, want %d", tt.s, got, tt.want)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Setenv("APP_NAME", "test-app")
	t.Setenv("APP_ENV", "testing")
	t.Setenv("NODE_ID", "test-node")
	t.Setenv("NODE_ROLE", "worker")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_NAME", "test_db")
	t.Setenv("DB_USER", "test_user")
	t.Setenv("NATS_URL", "nats://localhost:4222")
	defer os.Unsetenv("APP_NAME")
	defer os.Unsetenv("APP_ENV")
	defer os.Unsetenv("NODE_ID")
	defer os.Unsetenv("NODE_ROLE")
	defer os.Unsetenv("DB_HOST")
	defer os.Unsetenv("DB_NAME")
	defer os.Unsetenv("DB_USER")
	defer os.Unsetenv("NATS_URL")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.App.AppName != "test-app" {
		t.Errorf("AppName = %q, want %q", cfg.App.AppName, "test-app")
	}
	if cfg.App.AppEnv != "testing" {
		t.Errorf("AppEnv = %q, want %q", cfg.App.AppEnv, "testing")
	}
	if cfg.Node.ID != "test-node" {
		t.Errorf("Node.ID = %q, want %q", cfg.Node.ID, "test-node")
	}
	if cfg.Node.Role != "worker" {
		t.Errorf("Node.Role = %q, want %q", cfg.Node.Role, "worker")
	}
	if cfg.DB.Host != "localhost" {
		t.Errorf("DB.Host = %q, want %q", cfg.DB.Host, "localhost")
	}
	if cfg.DB.Name != "test_db" {
		t.Errorf("DB.Name = %q, want %q", cfg.DB.Name, "test_db")
	}
	if cfg.Nats.URL != "nats://localhost:4222" {
		t.Errorf("Nats.URL = %q, want %q", cfg.Nats.URL, "nats://localhost:4222")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	os.Unsetenv("APP_NAME")
	os.Unsetenv("APP_ENV")
	os.Unsetenv("NODE_ID")
	os.Unsetenv("NODE_ROLE")
	os.Unsetenv("DB_HOST")
	os.Unsetenv("DB_NAME")
	os.Unsetenv("DB_USER")
	os.Unsetenv("NATS_URL")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.App.AppName != "node-service" {
		t.Errorf("AppName = %q, want %q", cfg.App.AppName, "node-service")
	}
	if cfg.Node.ID != "node-1" {
		t.Errorf("Node.ID = %q, want %q", cfg.Node.ID, "node-1")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*ApplicationConfig)
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid",
			modify: func(c *ApplicationConfig) {
				c.Node.ID = "node-1"
				c.Node.Role = "entry"
				c.Node.HeartbeatInterval = 5 * time.Second
				c.Node.HeartbeatTimeout = 15 * time.Second
				c.Nats.URL = "nats://localhost:4222"
				c.DB.Host = "localhost"
				c.DB.Name = "test_db"
				c.DB.Username = "app"
			},
			wantErr: false,
		},
		{
			name: "missing_node_id",
			modify: func(c *ApplicationConfig) {
				c.Node.ID = ""
				c.Node.Role = "entry"
				c.Node.HeartbeatInterval = 5 * time.Second
				c.Node.HeartbeatTimeout = 15 * time.Second
			},
			wantErr: true,
			errMsg:  "NODE_ID is required",
		},
		{
			name: "missing_node_role",
			modify: func(c *ApplicationConfig) {
				c.Node.ID = "node-1"
				c.Node.Role = ""
				c.Node.HeartbeatInterval = 5 * time.Second
				c.Node.HeartbeatTimeout = 15 * time.Second
			},
			wantErr: true,
			errMsg:  "NODE_ROLE is required",
		},
		{
			name: "missing_nats_url",
			modify: func(c *ApplicationConfig) {
				c.Node.ID = "node-1"
				c.Node.Role = "entry"
				c.Node.HeartbeatInterval = 5 * time.Second
				c.Node.HeartbeatTimeout = 15 * time.Second
				c.Nats.URL = ""
			},
			wantErr: true,
			errMsg:  "NATS_URL is required",
		},
		{
			name: "missing_db_host",
			modify: func(c *ApplicationConfig) {
				c.Node.ID = "node-1"
				c.Node.Role = "entry"
				c.Node.HeartbeatInterval = 5 * time.Second
				c.Node.HeartbeatTimeout = 15 * time.Second
				c.Nats.URL = "nats://localhost:4222"
				c.DB.Host = ""
			},
			wantErr: true,
			errMsg:  "DB_HOST is required",
		},
		{
			name: "missing_db_name",
			modify: func(c *ApplicationConfig) {
				c.Node.ID = "node-1"
				c.Node.Role = "entry"
				c.Node.HeartbeatInterval = 5 * time.Second
				c.Node.HeartbeatTimeout = 15 * time.Second
				c.Nats.URL = "nats://localhost:4222"
				c.DB.Host = "localhost"
				c.DB.Name = ""
			},
			wantErr: true,
			errMsg:  "DB_NAME is required",
		},
		{
			name: "missing_db_user",
			modify: func(c *ApplicationConfig) {
				c.Node.ID = "node-1"
				c.Node.Role = "entry"
				c.Node.HeartbeatInterval = 5 * time.Second
				c.Node.HeartbeatTimeout = 15 * time.Second
				c.Nats.URL = "nats://localhost:4222"
				c.DB.Host = "localhost"
				c.DB.Name = "test"
				c.DB.Username = ""
			},
			wantErr: true,
			errMsg:  "DB_USER is required",
		},
		{
			name: "heartbeat_interval_too_small",
			modify: func(c *ApplicationConfig) {
				c.Node.ID = "node-1"
				c.Node.Role = "entry"
				c.Nats.URL = "nats://localhost:4222"
				c.DB.Host = "localhost"
				c.DB.Name = "test"
				c.DB.Username = "app"
				c.Node.HeartbeatInterval = 0
			},
			wantErr: true,
			errMsg:  "NODE_HEARTBEAT_INTERVAL must be positive",
		},
		{
			name: "heartbeat_timeout_less_than_interval",
			modify: func(c *ApplicationConfig) {
				c.Node.ID = "node-1"
				c.Node.Role = "entry"
				c.Nats.URL = "nats://localhost:4222"
				c.DB.Host = "localhost"
				c.DB.Name = "test"
				c.DB.Username = "app"
				c.Node.HeartbeatInterval = 10 * time.Second
				c.Node.HeartbeatTimeout = 5 * time.Second
			},
			wantErr: true,
			errMsg:  "NODE_HEARTBEAT_TIMEOUT must be greater than NODE_HEARTBEAT_INTERVAL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ApplicationConfig{}
			tt.modify(c)
			err := c.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() = nil, want error %q", tt.errMsg)
				} else if err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %q, want %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
			}
		})
	}
}

func TestLoadConfigWithEnv(t *testing.T) {
	t.Setenv("APP_NAME", "custom-app")
	t.Setenv("HTTP_PORT", "9090")
	t.Setenv("DB_MAX_OPEN_CONNS", "50")
	t.Setenv("OPERATION_TIMEOUT", "1h")
	t.Setenv("ELASTIC_ENABLED", "true")
	defer os.Unsetenv("APP_NAME")
	defer os.Unsetenv("HTTP_PORT")
	defer os.Unsetenv("DB_MAX_OPEN_CONNS")
	defer os.Unsetenv("OPERATION_TIMEOUT")
	defer os.Unsetenv("ELASTIC_ENABLED")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.App.AppName != "custom-app" {
		t.Errorf("AppName = %q, want %q", cfg.App.AppName, "custom-app")
	}
	if cfg.HTTP.Port != "9090" {
		t.Errorf("HTTP.Port = %q, want %q", cfg.HTTP.Port, "9090")
	}
	if cfg.DB.MaxOpenConns != 50 {
		t.Errorf("DB.MaxOpenConns = %d, want %d", cfg.DB.MaxOpenConns, 50)
	}
	if cfg.Operation.OperationTimeout != time.Hour {
		t.Errorf("OperationTimeout = %v, want %v", cfg.Operation.OperationTimeout, time.Hour)
	}
	if !cfg.Elastic.Enabled {
		t.Errorf("Elastic.Enabled = %v, want true", cfg.Elastic.Enabled)
	}
}

func TestGenerateIncarnationID(t *testing.T) {
	id := generateIncarnationID()
	if len(id) < 4 {
		t.Errorf("IncarnationID too short: %q", id)
	}
	if id[:3] != "inc" {
		t.Errorf("IncarnationID prefix = %q, want %q", id[:3], "inc")
	}

	id2 := generateIncarnationID()
	if id == id2 {
		t.Errorf("generateIncarnationID returned same ID twice: %q", id)
	}
}

func TestGetDurationEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		fallback time.Duration
		want     time.Duration
	}{
		{"zero_duration", "0s", time.Second, 0},
		{"negative_duration", "-1s", time.Second, -1 * time.Second},
		{"complex_duration", "1h30m", time.Minute, 1*time.Hour + 30*time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("TEST_COMPLEX_DURATION", tt.envVal)
			defer os.Unsetenv("TEST_COMPLEX_DURATION")
			got := GetDuration("TEST_COMPLEX_DURATION", tt.fallback)
			if got != tt.want {
				t.Errorf("GetDuration = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetIntEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		envVal   string
		fallback int
		want     int
	}{
		{"zero", "0", 1, 0},
		{"negative", "-5", 1, -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("TEST_INT_EDGE", tt.envVal)
			defer os.Unsetenv("TEST_INT_EDGE")
			got := GetInt("TEST_INT_EDGE", tt.fallback)
			if got != tt.want {
				t.Errorf("GetInt = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseSizeEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want int64
	}{
		{"0_KiB", "0KiB", 0},
		{"0_MiB", "0MiB", 0},
		{"0_GiB", "0GiB", 0},
		{"large_MiB", "512MiB", 512 * 1024 * 1024},
		{"large_GiB", "16GiB", 16 * 1024 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSize(tt.s)
			if got != tt.want {
				t.Errorf("ParseSize(%q) = %d, want %d", tt.s, got, tt.want)
			}
		})
	}
}

func TestGetDurationDefault(t *testing.T) {
	os.Unsetenv("TEST_DURATION_DEFAULT")
	got := GetDuration("TEST_DURATION_DEFAULT", 30*time.Second)
	if got != 30*time.Second {
		t.Errorf("GetDuration default = %v, want %v", got, 30*time.Second)
	}
}

func TestGetIntDefault(t *testing.T) {
	os.Unsetenv("TEST_INT_DEFAULT")
	got := GetInt("TEST_INT_DEFAULT", 100)
	if got != 100 {
		t.Errorf("GetInt default = %d, want %d", got, 100)
	}
}

func TestGetBoolDefault(t *testing.T) {
	os.Unsetenv("TEST_BOOL_DEFAULT")
	got := GetBool("TEST_BOOL_DEFAULT", true)
	if got != true {
		t.Errorf("GetBool default = %v, want true", got)
	}
}

func TestGetStringDefault(t *testing.T) {
	os.Unsetenv("TEST_STRING_DEFAULT")
	got := GetString("TEST_STRING_DEFAULT", "default_value")
	if got != "default_value" {
		t.Errorf("GetString default = %q, want %q", got, "default_value")
	}
}

func TestParseSizeDefaults(t *testing.T) {
	got := ParseSize("")
	if got != 1<<20 {
		t.Errorf("ParseSize empty = %d, want %d", got, 1<<20)
	}
}

func TestGetIntInvalidFormat(t *testing.T) {
	os.Setenv("TEST_INT_INVALID", "not_an_int_at_all")
	defer os.Unsetenv("TEST_INT_INVALID")
	got := GetInt("TEST_INT_INVALID", 42)
	if got != 42 {
		t.Errorf("GetInt with non-numeric env = %d, want fallback 42", got)
	}
}

func TestGetBoolInvalidFormat(t *testing.T) {
	os.Setenv("TEST_BOOL_INVALID", "not_a_bool")
	defer os.Unsetenv("TEST_BOOL_INVALID")
	got := GetBool("TEST_BOOL_INVALID", true)
	if got != true {
		t.Errorf("GetBool with non-bool env = %v, want fallback true", got)
	}
}

func TestLoadConfigFileConfigDefaults(t *testing.T) {
	os.Unsetenv("FILE_STAGING_DIR")
	os.Unsetenv("FILE_CHUNK_SIZE")
	os.Unsetenv("FILE_MAX_SIZE")
	os.Unsetenv("FILE_DOWNLOAD_CONCURRENCY")
	defer os.Unsetenv("FILE_STAGING_DIR")
	defer os.Unsetenv("FILE_CHUNK_SIZE")
	defer os.Unsetenv("FILE_MAX_SIZE")
	defer os.Unsetenv("FILE_DOWNLOAD_CONCURRENCY")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if cfg.File.Path != "/tmp/transfers" {
		t.Errorf("File.Path = %q, want %q", cfg.File.Path, "/tmp/transfers")
	}
	if cfg.File.ChunkSize != "1MiB" {
		t.Errorf("File.ChunkSize = %q, want %q", cfg.File.ChunkSize, "1MiB")
	}
	if cfg.File.MaxSize != "1GiB" {
		t.Errorf("File.MaxSize = %q, want %q", cfg.File.MaxSize, "1GiB")
	}
	if cfg.File.DownloadConcurrency != 4 {
		t.Errorf("File.DownloadConcurrency = %d, want %d", cfg.File.DownloadConcurrency, 4)
	}
}

func TestLoadConfigAllEnums(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("GIN_MODE", "release")
	t.Setenv("NODE_ROLE", "worker")
	t.Setenv("ELASTIC_ENABLED", "false")
	defer os.Unsetenv("APP_ENV")
	defer os.Unsetenv("GIN_MODE")
	defer os.Unsetenv("NODE_ROLE")
	defer os.Unsetenv("ELASTIC_ENABLED")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if cfg.App.AppEnv != "production" {
		t.Errorf("AppEnv = %q, want %q", cfg.App.AppEnv, "production")
	}
	if cfg.App.GinMode != "release" {
		t.Errorf("GinMode = %q, want %q", cfg.App.GinMode, "release")
	}
	if cfg.Node.Role != "worker" {
		t.Errorf("Node.Role = %q, want %q", cfg.Node.Role, "worker")
	}
	if cfg.Elastic.Enabled {
		t.Errorf("Elastic.Enabled = %v, want false", cfg.Elastic.Enabled)
	}
}

func TestGetDurationInvalidFormat(t *testing.T) {
	os.Setenv("TEST_DURATION_INVALID", "not_a_duration")
	defer os.Unsetenv("TEST_DURATION_INVALID")
	got := GetDuration("TEST_DURATION_INVALID", 5*time.Second)
	if got != 5*time.Second {
		t.Errorf("GetDuration with invalid format = %v, want fallback %v", got, 5*time.Second)
	}
}

func TestGetDurationFromEnv(t *testing.T) {
	t.Setenv("TEST_DURATION_ENV", "2m30s")
	defer os.Unsetenv("TEST_DURATION_ENV")
	got := GetDuration("TEST_DURATION_ENV", time.Second)
	if got != 2*time.Minute+30*time.Second {
		t.Errorf("GetDuration from env = %v, want %v", got, 2*time.Minute+30*time.Second)
	}
}

func TestGetIntFromEnv(t *testing.T) {
	t.Setenv("TEST_INT_ENV", "99")
	defer os.Unsetenv("TEST_INT_ENV")
	got := GetInt("TEST_INT_ENV", 0)
	if got != 99 {
		t.Errorf("GetInt from env = %d, want %d", got, 99)
	}
}

func TestParseSizeWithNoSuffix(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want int64
	}{
		{"bytes_100", "100", 100},
		{"bytes_1000", "1000", 1000},
		{"empty", "", 1 << 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSize(tt.s)
			if got != tt.want {
				t.Errorf("ParseSize(%q) = %d, want %d", tt.s, got, tt.want)
			}
		})
	}
}

func TestLoadConfigDBDefaults(t *testing.T) {
	os.Unsetenv("DB_HOST")
	os.Unsetenv("DB_PORT")
	os.Unsetenv("DB_NAME")
	os.Unsetenv("DB_USER")
	os.Unsetenv("DB_PASSWORD")
	defer os.Unsetenv("DB_HOST")
	defer os.Unsetenv("DB_PORT")
	defer os.Unsetenv("DB_NAME")
	defer os.Unsetenv("DB_USER")
	defer os.Unsetenv("DB_PASSWORD")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if cfg.DB.Host != "127.0.0.1" {
		t.Errorf("DB.Host = %q, want %q", cfg.DB.Host, "127.0.0.1")
	}
	if cfg.DB.Port != "3306" {
		t.Errorf("DB.Port = %q, want %q", cfg.DB.Port, "3306")
	}
	if cfg.DB.MaxOpenConns != 20 {
		t.Errorf("DB.MaxOpenConns = %d, want %d", cfg.DB.MaxOpenConns, 20)
	}
}

func TestLoadConfigOperationDefaults(t *testing.T) {
	os.Unsetenv("OPERATION_TIMEOUT")
	os.Unsetenv("HEALTH_CHECK_TIMEOUT")
	os.Unsetenv("MAX_HEALTH_RETRIES")
	defer os.Unsetenv("OPERATION_TIMEOUT")
	defer os.Unsetenv("HEALTH_CHECK_TIMEOUT")
	defer os.Unsetenv("MAX_HEALTH_RETRIES")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if cfg.Operation.OperationTimeout != 30*time.Minute {
		t.Errorf("OperationTimeout = %v, want %v", cfg.Operation.OperationTimeout, 30*time.Minute)
	}
	if cfg.Operation.HealthCheckTimeout != 15*time.Second {
		t.Errorf("HealthCheckTimeout = %v, want %v", cfg.Operation.HealthCheckTimeout, 15*time.Second)
	}
	if cfg.Operation.MaxHealthRetries != 3 {
		t.Errorf("MaxHealthRetries = %d, want %d", cfg.Operation.MaxHealthRetries, 3)
	}
}

func TestLoadConfigElasticDefaults(t *testing.T) {
	os.Unsetenv("ELASTIC_ENABLED")
	os.Unsetenv("ELASTIC_WORKERS")
	defer os.Unsetenv("ELASTIC_ENABLED")
	defer os.Unsetenv("ELASTIC_WORKERS")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if cfg.Elastic.Enabled {
		t.Errorf("Elastic.Enabled = %v, want false", cfg.Elastic.Enabled)
	}
	if cfg.Elastic.Workers != 2 {
		t.Errorf("Elastic.Workers = %d, want %d", cfg.Elastic.Workers, 2)
	}
}

func TestLoadConfigNodeDefaults(t *testing.T) {
	os.Unsetenv("NODE_ID")
	os.Unsetenv("NODE_HEARTBEAT_INTERVAL")
	defer os.Unsetenv("NODE_ID")
	defer os.Unsetenv("NODE_HEARTBEAT_INTERVAL")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if cfg.Node.ID != "node-1" {
		t.Errorf("Node.ID = %q, want %q", cfg.Node.ID, "node-1")
	}
	if cfg.Node.HeartbeatInterval != 5*time.Second {
		t.Errorf("HeartbeatInterval = %v, want %v", cfg.Node.HeartbeatInterval, 5*time.Second)
	}
}
