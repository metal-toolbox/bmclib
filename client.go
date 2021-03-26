// Package bmclib client.go is intended to be the main the public API.
// Its purpose is to make interacting with bmclib as friendly as possible.
package bmclib

import (
	"context"
	"io"

	"github.com/bmc-toolbox/bmclib/bmc"
	"github.com/bmc-toolbox/bmclib/logging"
	"github.com/bmc-toolbox/bmclib/providers/asrockrack"
	"github.com/bmc-toolbox/bmclib/providers/goipmi"
	"github.com/bmc-toolbox/bmclib/providers/ipmitool"
	"github.com/bmc-toolbox/bmclib/providers/redfish"
	"github.com/go-logr/logr"
	"github.com/jacobweinstock/registrar"
)

// Client for BMC interactions
type Client struct {
	Auth     Auth
	Logger   logr.Logger
	Registry *registrar.Registry
}

// Auth details for connecting to a BMC
type Auth struct {
	Host string
	Port string
	User string
	Pass string
}

// Option for setting optional Client values
type Option func(*Client)

// WithLogger sets the logger
func WithLogger(logger logr.Logger) Option {
	return func(args *Client) { args.Logger = logger }
}

// WithRegistry sets the Registry
func WithRegistry(registry *registrar.Registry) Option {
	return func(args *Client) { args.Registry = registry }
}

// NewClient returns a new Client struct
func NewClient(host, port, user, pass string, opts ...Option) *Client {
	var defaultClient = &Client{
		Logger:   logging.DefaultLogger(),
		Registry: registrar.NewRegistry(),
	}

	for _, opt := range opts {
		opt(defaultClient)
	}

	defaultClient.Registry.Logger = defaultClient.Logger
	defaultClient.Auth.Host = host
	defaultClient.Auth.Port = port
	defaultClient.Auth.User = user
	defaultClient.Auth.Pass = pass
	// len of 0 means that no Registry, with any registered providers was passed in.
	if len(defaultClient.Registry.Drivers) == 0 {
		defaultClient.registerProviders()
	}

	return defaultClient
}

func (c *Client) registerProviders() {
	// register ipmitool provider
	driverIpmitool := &ipmitool.Conn{Host: c.Auth.Host, Port: c.Auth.Port, User: c.Auth.User, Pass: c.Auth.Pass, Log: c.Logger}
	c.Registry.Register(ipmitool.ProviderName, ipmitool.ProviderProtocol, ipmitool.Features, nil, driverIpmitool)

	// register ASRR vendorapi provider
	driverAsrockrack, _ := asrockrack.New(c.Auth.Host, c.Auth.User, c.Auth.Pass, c.Logger)
	c.Registry.Register(asrockrack.ProviderName, asrockrack.ProviderProtocol, asrockrack.Features, nil, driverAsrockrack)

	// register goipmi provider
	driverGoIpmi := &goipmi.Conn{Host: c.Auth.Host, Port: c.Auth.Port, User: c.Auth.User, Pass: c.Auth.Pass, Log: c.Logger}
	c.Registry.Register(goipmi.ProviderName, goipmi.ProviderProtocol, goipmi.Features, nil, driverGoIpmi)

	// register gofish provider
	driverGoFish := &redfish.Conn{Host: c.Auth.Host, Port: c.Auth.Port, User: c.Auth.User, Pass: c.Auth.Pass, Log: c.Logger}
	c.Registry.Register(redfish.ProviderName, redfish.ProviderProtocol, redfish.Features, nil, driverGoFish)
	/*
		// dummy used for testing
		driverDummy := &dummy.Conn{FailOpen: true}
		c.Registry.Register(dummy.ProviderName, dummy.ProviderProtocol, dummy.Features, nil, driverDummy)
	*/
}

// Open calls the OpenConnectionFromInterfaces library function
// creates and returns a new Drivers with only implementations that were successfully opened
func (c *Client) Open(ctx context.Context, metadata ...*bmc.Metadata) (reg registrar.Drivers, err error) {
	ifs, err := bmc.OpenConnectionFromInterfaces(ctx, c.Registry.GetDriverInterfaces(), metadata...)
	if err != nil {
		return nil, err
	}
	for _, elem := range c.Registry.Drivers {
		for _, em := range ifs {
			if em == elem.DriverInterface {
				elem.DriverInterface = em
				reg = append(reg, elem)
			}
		}
	}
	return reg, nil
}

// Close pass through to library function
func (c *Client) Close(ctx context.Context, metadata ...*bmc.Metadata) (err error) {
	return bmc.CloseConnectionFromInterfaces(ctx, c.Registry.GetDriverInterfaces(), metadata...)
}

// GetPowerState pass through to library function
// if a metadata is passed in, it will be updated to be the name of the provider that successfully executed
func (c *Client) GetPowerState(ctx context.Context, metadata ...*bmc.Metadata) (state string, err error) {
	return bmc.GetPowerStateFromInterfaces(ctx, c.Registry.GetDriverInterfaces(), metadata...)
}

// SetPowerState pass through to library function
// if a metadata is passed in, it will be updated to be the name of the provider that successfully executed
func (c *Client) SetPowerState(ctx context.Context, state string, metadata ...*bmc.Metadata) (ok bool, err error) {
	return bmc.SetPowerStateFromInterfaces(ctx, state, c.Registry.GetDriverInterfaces(), metadata...)
}

// CreateUser pass through to library function
// if a metadata is passed in, it will be updated to be the name of the provider that successfully executed
func (c *Client) CreateUser(ctx context.Context, user, pass, role string, metadata ...*bmc.Metadata) (ok bool, err error) {
	return bmc.CreateUserFromInterfaces(ctx, user, pass, role, c.Registry.GetDriverInterfaces(), metadata...)
}

// UpdateUser pass through to library function
// if a metadata is passed in, it will be updated to be the name of the provider that successfully executed
func (c *Client) UpdateUser(ctx context.Context, user, pass, role string, metadata ...*bmc.Metadata) (ok bool, err error) {
	return bmc.UpdateUserFromInterfaces(ctx, user, pass, role, c.Registry.GetDriverInterfaces(), metadata...)
}

// DeleteUser pass through to library function
// if a metadata is passed in, it will be updated to be the name of the provider that successfully executed
func (c *Client) DeleteUser(ctx context.Context, user string, metadata ...*bmc.Metadata) (ok bool, err error) {
	return bmc.DeleteUserFromInterfaces(ctx, user, c.Registry.GetDriverInterfaces(), metadata...)
}

// ReadUsers pass through to library function
// if a metadata is passed in, it will be updated to be the name of the provider that successfully executed
func (c *Client) ReadUsers(ctx context.Context, metadata ...*bmc.Metadata) (users []map[string]string, err error) {
	return bmc.ReadUsersFromInterfaces(ctx, c.Registry.GetDriverInterfaces(), metadata...)
}

// SetBootDevice pass through to library function
// if a metadata is passed in, it will be updated to be the name of the provider that successfully executed
func (c *Client) SetBootDevice(ctx context.Context, bootDevice string, setPersistent, efiBoot bool, metadata ...*bmc.Metadata) (ok bool, err error) {
	return bmc.SetBootDeviceFromInterfaces(ctx, bootDevice, setPersistent, efiBoot, c.Registry.GetDriverInterfaces(), metadata...)
}

// ResetBMC pass through to library function
// if a metadata is passed in, it will be updated to be the name of the provider that successfully executed
func (c *Client) ResetBMC(ctx context.Context, resetType string, metadata ...*bmc.Metadata) (ok bool, err error) {
	return bmc.ResetBMCFromInterfaces(ctx, resetType, c.Registry.GetDriverInterfaces(), metadata...)
}

// GetBMCVersion pass through library function
func (c *Client) GetBMCVersion(ctx context.Context) (version string, err error) {
	return bmc.GetBMCVersionFromInterfaces(ctx, c.Registry.GetDriverInterfaces())
}

// UpdateBMCFirmware pass through library function
func (c *Client) UpdateBMCFirmware(ctx context.Context, fileReader io.Reader, fileSize int64) (err error) {
	return bmc.UpdateBMCFirmwareFromInterfaces(ctx, fileReader, fileSize, c.Registry.GetDriverInterfaces())
}

// GetBIOSVersion pass through library function
func (c *Client) GetBIOSVersion(ctx context.Context) (version string, err error) {
	return bmc.GetBIOSVersionFromInterfaces(ctx, c.Registry.GetDriverInterfaces())
}

// UpdateBIOSFirmware pass through library function
func (c *Client) UpdateBIOSFirmware(ctx context.Context, fileReader io.Reader, fileSize int64) (err error) {
	return bmc.UpdateBIOSFirmwareFromInterfaces(ctx, fileReader, fileSize, c.Registry.GetDriverInterfaces())
}