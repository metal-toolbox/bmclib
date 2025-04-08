# Bios Configuration Apply

This example applies the bios configuration specified in the XML. For Dell
servers, this is the same XML that would be supplied to `racadm` and for
Supermicro servers, this is the same XML as would be supplied to `sum`.

Example:
```
go run examples/bios/main.go -user <username> -password <bmc password> -host <bmc ip> -mode setfile -file <bios configuration>.xml
```
