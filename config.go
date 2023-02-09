package mongostore

// Config mongodb configuration parameters
type Config struct {
	Host          string
	Port          int
	Source        string
	Collection    string
	AuthMechanism string
	Username      string
	Password      string
	AuthSource    string
	Auth          bool
}

// NewConfig create mongodb configuration
func NewConfig(host, source, collection, username, password, authSource string, port int) *Config {
	return &Config{
		Host:          host,
		Port:          port,
		Source:        source,
		Collection:    collection,
		AuthMechanism: "SCRAM-SHA-1",
		Username:      username,
		Password:      password,
		AuthSource:    authSource,
		Auth:          false,
	}
}
