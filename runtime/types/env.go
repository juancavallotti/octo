package types

// EnvVar declares an environment variable the config depends on. Variables must be
// declared here before they can be referenced as ${NAME} in any settings value, so
// every external input a config relies on is documented in one place.
//
// Resolution precedence is OS environment > .env file > Default. A variable marked
// Required must be supplied by the OS environment or a .env file; a Default does not
// satisfy it.
type EnvVar struct {
	Name string `yaml:"name"`
	// Default is the value used when neither the OS environment nor a .env file
	// supplies the variable. The pointer distinguishes an absent default (nil, so a
	// referenced-but-unresolved variable is an error) from an explicit empty
	// default (a value of "").
	Default *string `yaml:"default,omitempty"`
	// Required fails the load when the variable is not supplied by the OS
	// environment or a .env file.
	Required bool `yaml:"required,omitempty"`
}
