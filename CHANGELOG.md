# Changelog

## v1.0.33

- Update dependencies

## v1.0.32

- Update dependencies

## v1.0.31
- `Responses contain` verification input was changed to a textarea to allow multi-line inputs.

## v1.0.30

- Fix: `Responses contain` verification parameters were renamed with v1.0.29. Existing experiment design will not verify the response if a paramter was set. This fix will revert the change and use the old parameter name.
  - If you have designed experiments with HTTP check `v1.0.29` and used the `Responses contain` verification parameter, you need to migrate experiment designs after updating to `v1.0.30`.
  - **Migration for SaaS Customers** Please reach out to us.
  - **Migration for On-Prem Customers**
    - How to check whether you're affected? If the query below returns any rows, you need to migrate after updating to `v1.0.30` and having a database backup in place
        ```sql
        SELECT es.experiment_key, es.custom_label, esa.action_id, es.parameters
          FROM sb_onprem.experiment_step es JOIN sb_onprem.experiment_step_attack esa ON es.id = esa.id
          WHERE esa.action_id IN ('com.steadybit.extension_http.check.periodically', 'com.steadybit.extension_http.check.fixed_amount')
          AND parameters ? 'responsesContain';
        ```
    - How to migrate existing experiments? After you've done a database backup, execute the following SQL
        ```sql
         UPDATE sb_onprem.experiment_step SET parameters = jsonb_set(parameters - 'responsesContain','{responsesContains}', parameters -> 'responsesContain')
           WHERE id IN (
             SELECT es.id
               FROM sb_onprem.experiment_step es JOIN sb_onprem.experiment_step_attack esa ON es.id = esa.id
               WHERE esa.action_id IN ('com.steadybit.extension_http.check.periodically', 'com.steadybit.extension_http.check.fixed_amount')
               AND parameters ? 'responsesContain'
           );
        ```

## v1.0.29

- updated depencies

## v1.0.28

- chore: trace logging for requests/responses

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