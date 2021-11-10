package idrac8

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"

	"github.com/bmc-toolbox/bmclib/cfgresources"
	"github.com/bmc-toolbox/bmclib/devices"
	"github.com/bmc-toolbox/bmclib/internal"
	"github.com/bmc-toolbox/bmclib/internal/helper"
)

// This ensures the compiler errors if this type is missing
// a method that should be implmented to satisfy the Configure interface.
var _ devices.Configure = (*IDrac8)(nil)

// Resources returns a slice of supported resources and
// the order they are to be applied in.
func (i *IDrac8) Resources() []string {
	return []string{
		"user",
		"syslog",
		"network",
		"ntp",
		"ldap",
		"ldap_group",
		"https_cert",
	}
}

// Power implemented the Configure interface
func (i *IDrac8) Power(cfg *cfgresources.Power) error {
	return nil
}

// SetLicense implements the Configure interface.
func (i *IDrac8) SetLicense(cfg *cfgresources.License) (err error) {
	return err
}

// Bios implements the Configure interface.
func (i *IDrac8) Bios(cfg *cfgresources.Bios) (err error) {
	return err
}

func escapeLdapString(s string) string {
	r := ""
	for _, c := range s {
		if c == '=' {
			r += "%5C%3D"
		} else if c == ',' {
			r += "%5C%2C"
		} else {
			r += string(c)
		}
	}

	return r
}

// User applies the User configuration resource,
// if the user exists, it updates the users password,
// User implements the Configure interface.
// Iterate over iDrac users and adds/removes/modifies user accounts
func (i *IDrac8) User(cfgUsers []*cfgresources.User) (err error) {
	err = internal.ValidateUserConfig(cfgUsers)
	if err != nil {
		msg := "User config validation failed: " + err.Error()
		err = errors.New(msg)
		i.log.V(1).Error(err, msg,
			"step", "applyUserParams",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
		)
		return err
	}

	idracUsers, err := i.queryUsers()
	if err != nil {
		msg := "Unable to query existing users."
		err = errors.New(msg)
		i.log.V(1).Error(err, msg,
			"step", "applyUserParams",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
		)
		return err
	}

	for _, cfgUser := range cfgUsers {

		userID, userInfo, uExists := userInIdrac(cfgUser.Name, idracUsers)
		if cfgUser.Enable {
			// New user? Add them.
			if !uExists {
				userID, userInfo, err = getEmptyUserSlot(idracUsers)
				if err != nil {
					i.log.V(1).Error(err, "Unable to add new User.",
						"IP", i.ip,
						"HardwareType", i.HardwareType(),
						"step", helper.WhosCalling(),
						"User", cfgUser.Name,
					)
					continue
				}
			}

			userInfo.Enable = "Enabled"
			userInfo.SolEnable = "Enabled"
			userInfo.UserName = cfgUser.Name
			userInfo.Password = cfgUser.Password

			if cfgUser.Role == "admin" {
				userInfo.Privilege = "511"
				userInfo.IpmiLanPrivilege = "Administrator"
			} else {
				userInfo.Privilege = "499"
				userInfo.IpmiLanPrivilege = "Operator"
			}

			err = i.putUser(userID, userInfo)
			if err != nil {
				i.log.V(1).Error(err, "User(): Add/Update user request failed.",
					"IP", i.ip,
					"HardwareType", i.HardwareType(),
					"step", helper.WhosCalling(),
					"User", cfgUser.Name,
				)
				continue
			}
		}

		// User exists but is disabled in our config? Remove them.
		if !cfgUser.Enable && uExists {

			userInfo.Enable = "Disabled"
			userInfo.SolEnable = "Disabled"
			userInfo.UserName = cfgUser.Name
			userInfo.Privilege = "0"
			userInfo.IpmiLanPrivilege = "No Access"

			err = i.putUser(userID, userInfo)
			if err != nil {
				i.log.V(1).Error(err, "User(): Disable user request failed.",
					"IP", i.ip,
					"HardwareType", i.HardwareType(),
					"step", helper.WhosCalling(),
					"User", cfgUser.Name,
				)
			}
		}

		i.log.V(1).Info("User parameters applied.", "IP", i.ip, "HardwareType", i.HardwareType(), "User", cfgUser.Name)
	}

	return err
}

// Syslog applies the Syslog configuration resource
// Syslog implements the Configure interface
//
// As part of Syslog we enable alerts and alert filters to syslog,
// the iDrac will not send out any messages over syslog unless this is enabled,
// and since not all BMCs currently support configuring filtering for alerts,
// for now the configuration for alert filters/enabling is managed through this method.
func (i *IDrac8) Syslog(cfg *cfgresources.Syslog) (err error) {
	var port int
	enable := "Enabled"

	if cfg.Server == "" {
		i.log.V(1).Info("Syslog resource expects parameter: Server.", "step", helper.WhosCalling())
		return
	}

	if cfg.Port == 0 {
		i.log.V(1).Info("Syslog resource port set to default: 514.", "step", helper.WhosCalling())
		port = 514
	} else {
		port = cfg.Port
	}

	if !cfg.Enable {
		enable = "Disabled"
		i.log.V(1).Info("Syslog resource declared with enable: false.", "step", helper.WhosCalling())
	}

	data := make(map[string]Syslog)

	data["iDRAC.SysLog"] = Syslog{
		Port:    strconv.Itoa(port),
		Server1: cfg.Server,
		Server2: "",
		Server3: "",
		Enable:  enable,
	}

	payload, err := json.Marshal(data)
	if err != nil {
		i.log.V(1).Error(err, "Unable to marshal syslog payload.", "step", helper.WhosCalling())
		return err
	}

	endpoint := "sysmgmt/2012/server/configgroup/iDRAC.SysLog"
	response, _, err := i.put(endpoint, payload)
	if err != nil {
		i.log.V(1).Error(err, "request to set syslog configuration failed.",
			"endpoint", endpoint,
			"step", helper.WhosCalling(),
			"response", fmt.Sprint(response),
		)
		return err
	}

	// enable alerts
	endpoint = "data?set=alertStatus:1"
	response, _, err = i.post(endpoint, []byte{}, "")
	if err != nil {
		i.log.V(1).Error(err, "request to enable alerts failed.",
			"endpoint", endpoint,
			"step", helper.WhosCalling(),
			"response", fmt.Sprint(response),
		)
		return err
	}

	// setup alert filters
	endpoint = "data?set=" + setAlertFilterPayload
	response, _, err = i.post(endpoint, []byte{}, "")
	if err != nil {
		i.log.V(1).Error(err, "request to set alerts filter configuration failed.",
			"endpoint", endpoint,
			"step", helper.WhosCalling(),
			"response", fmt.Sprint(response),
		)
		return err
	}

	i.log.V(1).Info("Syslog parameters applied.", "IP", i.ip, "HardwareType", i.HardwareType())

	return err
}

// Ntp applies NTP configuration params
// Ntp implements the Configure interface.
func (i *IDrac8) Ntp(cfg *cfgresources.Ntp) (err error) {
	if cfg.Server1 == "" {
		i.log.V(1).Info("NTP resource expects parameter: server1.", "step", "apply-ntp-cfg")
		return
	}

	if cfg.Timezone == "" {
		i.log.V(1).Info("NTP resource expects parameter: timezone.", "step", "apply-ntp-cfg")
		return
	}

	i.applyTimezoneParam(cfg.Timezone)
	i.applyNtpServerParam(cfg)

	return err
}

func (i *IDrac8) applyNtpServerParam(cfg *cfgresources.Ntp) {
	var enable int
	if !cfg.Enable {
		i.log.V(1).Info("Ntp resource declared with enable: false.", "step", helper.WhosCalling())
		enable = 0
	} else {
		enable = 1
	}

	// https://10.193.251.10/data?set=tm_ntp_int_opmode:1,
	//                               tm_ntp_str_server1:ntp0.lhr4.example.com,
	//                               tm_ntp_str_server2:ntp0.ams4.example.com,
	//                               tm_ntp_str_server3:ntp0.fra4.example.com
	queryStr := fmt.Sprintf("set=tm_ntp_int_opmode:%d,", enable)
	queryStr += fmt.Sprintf("tm_ntp_str_server1:%s,", cfg.Server1)
	queryStr += fmt.Sprintf("tm_ntp_str_server2:%s,", cfg.Server2)
	queryStr += fmt.Sprintf("tm_ntp_str_server3:%s,", cfg.Server3)

	endpoint := fmt.Sprintf("data?%s", queryStr)
	statusCode, response, err := i.get(endpoint, nil)
	if err != nil || statusCode != 200 {
		if err == nil {
			err = fmt.Errorf("Received a non-200 status code from the GET request to %s.", endpoint)
		}

		i.log.V(1).Error(err, "applyNtpServerParam(): GET request failed.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"endpoint", endpoint,
			"StatusCode", statusCode,
			"step", helper.WhosCalling(),
			"response", string(response),
		)
	}

	i.log.V(1).Info("NTP servers param applied.", "IP", i.ip, "HardwareType", i.HardwareType())
}

// Ldap applies LDAP configuration params.
// Ldap implements the Configure interface.
func (i *IDrac8) Ldap(cfg *cfgresources.Ldap) error {
	if cfg.Server == "" {
		msg := "LDAP resource parameter \"Server\" required but not declared."
		err := errors.New(msg)
		i.log.V(1).Error(err, msg, "step", "applyLdapServerParam")
		return err
	}

	endpoint := fmt.Sprintf("data?set=xGLServer:%s", cfg.Server)
	statusCode, response, err := i.get(endpoint, nil)
	if err != nil || statusCode != 200 {
		if err == nil {
			err = fmt.Errorf("Received a non-200 status code from the GET request to %s.", endpoint)
		}
		i.log.V(1).Error(err, "Request to set LDAP server failed.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"endpoint", endpoint,
			"StatusCode", statusCode,
			"step", helper.WhosCalling(),
			"response", string(response),
		)
		return err
	}

	err = i.applyLdapSearchFilterParam(cfg)
	if err != nil {
		return err
	}

	i.log.V(1).Info("Ldap server param set.", "IP", i.ip, "HardwareType", i.HardwareType())
	return nil
}

// Applies ldap search filter param.
// set=xGLSearchFilter:objectClass\=posixAccount
func (i *IDrac8) applyLdapSearchFilterParam(cfg *cfgresources.Ldap) error {
	if cfg.SearchFilter == "" {
		msg := "Ldap resource parameter SearchFilter required but not declared."
		err := errors.New(msg)
		i.log.V(1).Error(err, msg, "step", "applyLdapSearchFilterParam")
		return err
	}

	endpoint := fmt.Sprintf("data?set=xGLSearchFilter:%s", escapeLdapString(cfg.SearchFilter))
	statusCode, response, err := i.get(endpoint, nil)
	if err != nil || statusCode != 200 {
		if err == nil {
			err = fmt.Errorf("Received a non-200 status code from the GET request to %s.", endpoint)
		}

		i.log.V(1).Error(err, "Request to set LDAP search filter failed.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"endpoint", endpoint,
			"StatusCode", statusCode,
			"step", helper.WhosCalling(),
			"response", string(response),
		)
		return err
	}

	i.log.V(1).Info("Ldap search filter param applied.", "IP", i.ip, "HardwareType", i.HardwareType())
	return nil
}

// Applies LDAP Group/Role related configuration.
// Implements the Configure interface.
func (i *IDrac8) LdapGroups(cfgGroups []*cfgresources.LdapGroup, cfgLdap *cfgresources.Ldap) (err error) {
	groupID := 1

	// set to decide what privileges the group should have
	// 497 == operator
	// 511 == administrator (full privileges)
	privID := "0"

	// groupPrivilegeParam is populated per group and is passed to i.applyLdapRoleGroupPrivParam
	groupPrivilegeParam := ""

	// first some preliminary checks
	if cfgLdap.Port == 0 {
		msg := "LDAP resource parameter \"Port\" is required!"
		err = errors.New(msg)
		i.log.V(1).Error(err, msg, "step", "applyLdapRoleGroupPrivParam")
		return err
	}

	if cfgLdap.BaseDn == "" {
		msg := "LDAP resource parameter \"BaseDn\" is required!"
		err = errors.New(msg)
		i.log.V(1).Error(err, msg, "step", "applyLdapRoleGroupPrivParam")
		return err
	}

	if cfgLdap.UserAttribute == "" {
		msg := "LDAP resource parameter \"userAttribute\" is required!"
		err = errors.New(msg)
		i.log.V(1).Error(err, msg, "step", "applyLdapRoleGroupPrivParam")
		return err
	}

	if cfgLdap.GroupAttribute == "" {
		msg := "LDAP resource parameter \"groupAttribute\" is required!"
		err = errors.New(msg)
		i.log.V(1).Error(err, msg, "step", "applyLdapRoleGroupPrivParam")
		return err
	}

	for _, group := range cfgGroups {
		// if a group has been set to disable in the config,
		// its configuration is skipped and removed.
		if !group.Enable {
			continue
		}

		if group.Role == "" {
			msg := "Ldap resource parameter Role required but not declared."
			i.log.V(1).Info(msg, "Role", group.Role, "step", "applyLdapGroupParams")
			continue
		}

		if group.Group == "" {
			msg := "LDAP resource parameter \"Group\" is required!"
			err = errors.New(msg)
			i.log.V(1).Error(err, msg, "step", "applyLdapGroupParams")
			return err
		}

		if group.GroupBaseDn == "" {
			msg := "LDAP resource parameter \"GroupBaseDn\" is required!"
			err = errors.New(msg)
			i.log.V(1).Error(err, msg, "step", "applyLdapGroupParams")
			return err
		}

		if !internal.IsRoleValid(group.Role) {
			msg := "LDAP resource parameter \"Role\" must be a valid role: \"admin\" OR \"user\"."
			err = errors.New(msg)
			i.log.V(1).Error(err, msg, "Role", group.Role, "step", "applyLdapGroupParams")
			return err
		}

		groupDn := fmt.Sprintf("%s,%s", group.Group, group.GroupBaseDn)
		groupDn = escapeLdapString(groupDn)

		endpoint := fmt.Sprintf("data?set=xGLGroup%dName:%s", groupID, groupDn)
		statusCode, response, err := i.get(endpoint, nil)
		if err != nil || statusCode != 200 {
			if err == nil {
				err = fmt.Errorf("Received a non-200 status code from the GET request to %s.", endpoint)
			}

			i.log.V(1).Error(err, "LdapGroups(): GET request failed.",
				"IP", i.ip,
				"HardwareType", i.HardwareType(),
				"endpoint", endpoint,
				"StatusCode", statusCode,
				"step", "applyLdapGroupParams",
				"response", string(response),
			)
			return err
		}

		i.log.V(1).Info("LDAP GroupDN config applied.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"Role", group.Role,
		)

		switch group.Role {
		case "user":
			privID = "497"
		case "admin":
			privID = "511"
		}

		groupPrivilegeParam += fmt.Sprintf("xGLGroup%dPriv:%s,", groupID, privID)
		groupID++
	}

	// Set the rest of the group privileges to 0.
	// Dell supports only 5 groups.
	for i := groupID; i <= 5; i++ {
		groupPrivilegeParam += fmt.Sprintf("xGLGroup%dPriv:0,", i)
	}

	err = i.applyLdapRoleGroupPrivParam(cfgLdap, groupPrivilegeParam)
	if err != nil {
		i.log.V(1).Error(err, "Unable to set Ldap Role Group Privileges.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"step", "applyLdapGroupParams",
		)
		return err
	}
	return err
}

// Apply ldap group privileges
// https://10.193.251.10/postset?ldapconf
// data=LDAPEnableMode:3,xGLNameSearchEnabled:0,xGLBaseDN:ou%5C%3DPeople%5C%2Cdc%5C%3Dactivehotels%5C%2Cdc%5C%3Dcom,xGLUserLogin:uid,xGLGroupMem:memberUid,xGLBindDN:,xGLCertValidationEnabled:1,xGLGroup1Priv:511,xGLGroup2Priv:97,xGLGroup3Priv:0,xGLGroup4Priv:0,xGLGroup5Priv:0,xGLServerPort:636
func (i *IDrac8) applyLdapRoleGroupPrivParam(cfg *cfgresources.Ldap, groupPrivilegeParam string) (err error) {
	baseDn := escapeLdapString(cfg.BaseDn)
	payload := "data=LDAPEnableMode:3,"  // Generic LDAP
	payload += "xGLNameSearchEnabled:0," // Lookup LDAP server from DNS
	payload += fmt.Sprintf("xGLBaseDN:%s,", baseDn)
	payload += fmt.Sprintf("xGLUserLogin:%s,", cfg.UserAttribute)
	payload += fmt.Sprintf("xGLGroupMem:%s,", cfg.GroupAttribute)

	if cfg.BindDn != "" {
		bindDn := escapeLdapString(cfg.BindDn)
		payload += fmt.Sprintf("xGLBindDN:%s,", bindDn)
	} else {
		payload += "xGLBindDN:,"
	}

	payload += "xGLCertValidationEnabled:0," // TODO: Set this from config?
	payload += groupPrivilegeParam
	payload += fmt.Sprintf("xGLServerPort:%d", cfg.Port)

	endpoint := "postset?ldapconf"
	responseCode, responseBody, err := i.post(endpoint, []byte(payload), "")
	if err != nil || responseCode != 200 {
		i.log.V(1).Error(err, "POST request failed.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"endpoint", endpoint,
			"step", helper.WhosCalling(),
			"responseCode", responseCode,
			"response", string(responseBody),
		)
		return err
	}

	i.log.V(1).Info("LDAP Group role privileges applied.", "IP", i.ip, "HardwareType", i.HardwareType())

	return err
}

func (i *IDrac8) applyTimezoneParam(timezone string) {
	// POST - params as query string
	// https://10.193.251.10/data?set=tm_tz_str_zone:CET

	endpoint := fmt.Sprintf("data?set=tm_tz_str_zone:%s", timezone)
	statusCode, response, err := i.get(endpoint, nil)
	if err != nil || statusCode != 200 {
		if err == nil {
			err = fmt.Errorf("Received a non-200 status code from the GET request to %s.", endpoint)
		}

		i.log.V(1).Error(err, "applyTimezoneParam(): GET request failed.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"endpoint", endpoint,
			"StatusCode", statusCode,
			"step", helper.WhosCalling(),
			"response", string(response),
		)
	}

	i.log.V(1).Info("Timezone param applied.", "IP", i.ip, "HardwareType", i.HardwareType())
}

// Network method implements the Configure interface
// applies various network parameters.
func (i *IDrac8) Network(cfg *cfgresources.Network) (reset bool, err error) {
	params := map[string]int{
		"EnableIPv4":              1,
		"DHCPEnable":              1,
		"DNSFromDHCP":             1,
		"EnableSerialOverLan":     1,
		"EnableSerialRedirection": 1,
		"EnableIpmiOverLan":       1,
	}

	if !cfg.DNSFromDHCP {
		params["DNSFromDHCP"] = 0
	}

	if !cfg.IpmiEnable {
		params["EnableIpmiOverLan"] = 0
	}

	if !cfg.SolEnable {
		params["EnableSerialOverLan"] = 0
		params["EnableSerialRedirection"] = 0
	}

	endpoint := "data?set"
	payload := fmt.Sprintf("dhcpForDNSDomain:%d,", params["DNSFromDHCP"])
	payload += fmt.Sprintf("ipmiLAN:%d,", params["EnableIpmiOverLan"])
	payload += fmt.Sprintf("serialOverLanEnabled:%d,", params["EnableSerialOverLan"])
	payload += "serialOverLanBaud:3," // 115.2 kbps
	payload += "serialOverLanPriv:0," // Administrator
	payload += fmt.Sprintf("racRedirectEna:%d,", params["EnableSerialRedirection"])
	payload += "racEscKey:^\\\\"

	responseCode, responseBody, err := i.post(endpoint, []byte(payload), "")
	if err != nil || responseCode != 200 {
		i.log.V(1).Error(err, "POST request to set Network params failed.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"endpoint", endpoint,
			"step", helper.WhosCalling(),
			"responseCode", responseCode,
			"response", string(responseBody),
		)
		return reset, err
	}

	i.log.V(1).Info("Network config parameters applied.", "IP", i.ip, "HardwareType", i.HardwareType())
	return reset, err
}

// GenerateCSR generates a CSR request on the BMC.
func (i *IDrac8) GenerateCSR(cert *cfgresources.HTTPSCertAttributes) ([]byte, error) {
	var payload []string

	endpoint := "bindata?set"
	payload = []string{
		cert.CommonName,
		cert.OrganizationName,
		cert.OrganizationUnit,
		cert.Locality,
		cert.StateName,
		cert.CountryCode,
		strings.Join(strings.Split(cert.Email, "@"), "@040"), // heh, don't ask.
		cert.SubjectAltName,
	}

	queryString := url.QueryEscape(fmt.Sprintf("%s=serverCSR(%s)", endpoint, strings.Join(payload, ",")))

	statusCode, response, err := i.get(queryString, nil)
	if err != nil || statusCode != 200 {
		if err == nil {
			err = fmt.Errorf("Received a non-200 status code from the GET request to %s.", endpoint)
		}

		i.log.V(1).Error(err, "GenerateCSR(): GET request failed.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"endpoint", endpoint,
			"StatusCode", statusCode,
			"step", helper.WhosCalling(),
		)
		return []byte{}, err
	}

	return response, nil
}

// UploadHTTPSCert uploads the given CRT cert,
// returns true if the BMC needs a reset.
// 1. POST upload signed x509 cert in multipart form.
// 2. POST returned resource URI
func (i *IDrac8) UploadHTTPSCert(cert []byte, certFileName string, key []byte, keyFileName string) (bool, error) {
	endpoint := "sysmgmt/2012/server/transient/filestore?fileupload=true"
	endpoint += fmt.Sprintf("&ST1=%s", i.st1)

	// form params
	params := make(map[string]string)
	params["caller"] = ""
	params["pageCode"] = ""
	params["pageId"] = "2"
	params["pageName"] = ""
	params["index"] = "8"

	// setup a buffer for our multipart form
	var form bytes.Buffer
	w := multipart.NewWriter(&form)

	// write params to form
	for k, v := range params {
		_ = w.WriteField(k, v)
	}

	// setup the ssl cert part
	formWriter, err := w.CreateFormFile("serverSSLCertificate", certFileName)
	if err != nil {
		return false, err
	}

	_, err = io.Copy(formWriter, bytes.NewReader(cert))
	if err != nil {
		return false, err
	}

	_ = w.WriteField("CertType", "2")

	// close multipart writer - adds the teminating boundary.
	w.Close()

	// 1. POST upload x509 cert
	status, body, err := i.post(endpoint, form.Bytes(), w.FormDataContentType())
	if err != nil || status != 201 {
		if err == nil {
			err = fmt.Errorf("Cert form upload POST request to %s failed, expected 201.", endpoint)
		}

		i.log.V(1).Error(err, "UploadHTTPSCert(): Cert form upload POST request failed.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"endpoint", endpoint,
			"step", helper.WhosCalling(),
			"StatusCode", status,
		)
		return false, err
	}

	// extract resourceURI from response
	certStore := new(certStore)
	err = json.Unmarshal(body, certStore)
	if err != nil {
		i.log.V(1).Error(err, "UploadHTTPSCert(): Unable to unmarshal cert store response payload.",
			"step", helper.WhosCalling(),
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
		)
		return false, err
	}

	resourceURI, err := json.Marshal(certStore.File)
	if err != nil {
		i.log.V(1).Error(err, "UploadHTTPSCert(): Unable to marshal cert store resource URI.",
			"step", helper.WhosCalling(),
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
		)
		return false, err
	}

	// 2. POST resource URI
	endpoint = "sysmgmt/2012/server/network/ssl/cert"
	status, _, err = i.post(endpoint, []byte(resourceURI), "")
	if err != nil || status != 201 {
		if err == nil {
			err = fmt.Errorf("Cert form upload POST request to %s failed, expected 201.", endpoint)
		}

		i.log.V(1).Error(err, "UploadHTTPSCert(): Cert form upload POST request failed.",
			"IP", i.ip,
			"HardwareType", i.HardwareType(),
			"endpoint", endpoint,
			"step", helper.WhosCalling(),
			"StatusCode", status,
		)
		return false, err
	}

	return true, err
}
