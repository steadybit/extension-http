# Changelog

## v1.0.27

- ability to import own certificates for TLS connections
- ability to ignore TLS errors for http connections
- Updated dependencies

## v1.0.26

- Fix success rate not correctly calculated when there are unfinished requests

## v1.0.25

- Add hint describing the behavior of the fixed amount check and lower the default duration to 2 seconds
- Fix memory lead in the http check

## v1.0.24

- Update dependencies
- Failing HTTP requests are shown yellow instead of red

## v1.0.23

- Location selection for http checks (can be enabled via STEADYBIT_EXTENSION_ENABLE_LOCATION_SELECTION env var, requires platform => 2.1.27)
- Use "error" in the expected HTTP status code field to verify that requests are returning an error
- Use uid instead of name for user statement in Dockerfile

## v1.0.22

- Update dependencies (go 1.23)

## v1.0.21

- Added an option to verify response times

## v1.0.20

- Update dependencies

## v1.0.19

- Update dependencies (go 1.22)
- Improved status code constraint parsing

## v1.0.18

- Update dependencies

## v1.0.17

- Update dependencies

## v1.0.16

- Update dependencies

## v1.0.15

- Update dependencies

## v1.0.14

- Update dependencies
- Fix response time calculation

## v1.0.13

- Fix "Response Contains" check

## v1.0.12

- Possibility to exclude attributes from discovery

## v1.0.11

- update dependencies

## v1.0.10

- migration to new unified steadybit actionIds and targetTypes

## v1.0.9

- update dependencies

## v1.0.8

 - fix invalid url causing panics
 - fixes for the linux packaging

## v1.0.7

 - add support for unix domain sockets

## v1.0.6

 - use read only file system

## v1.0.5

 - fix: success rate calculation

## v1.0.3

 - Renaming the actions

## v1.0.2

 - Some cleanup

## v1.0.1

 - Initial release
