{{/*
Get Pod Env
- Merges default environment variables (if used) with component environment variables.
- If using defaults, will pull out OTEL_RESOURCE_ATTRIBUTES from the list to be reused later.
- An environment variable named OTEL_RESOURCE_ATTRIBUTES_EXTRA will have its value appended to the value of the
OTEL_RESOURCE_ATTRIBUTES environment variable if it exists.
- The OTEL_RESOURCES_ATTRIBUTES environment variable will typically use Kubernetes environment variable expansion and
should be last.
*/}}
{{- define "otel-demo.pod.env" -}}
{{- $resourceAttributesEnv := dict }}
{{- $allEnvs := list }}

{{- if .useDefault.env  }}
{{-   $defaultEnvs := include "otel-demo.envOverriden" (dict "env" .defaultValues.env "envOverrides" .defaultValues.envOverrides) | mustFromJson }}
{{-   range $defaultEnvs }}
{{-     if eq .name "OTEL_RESOURCE_ATTRIBUTES" }}
{{-       $resourceAttributesEnv = . }}
{{-     else }}
{{-       $allEnvs = append $allEnvs . }}
{{-     end }}
{{-   end }}
{{- end }}

{{- if or .env .envOverrides }}
{{-   $localEnvs := include "otel-demo.envOverriden" . | mustFromJson }}
{{-   range $localEnvs }}
{{-     if eq .name "OTEL_RESOURCE_ATTRIBUTES" }}
{{-       $resourceAttributesEnv = . }}
{{-     else if and $resourceAttributesEnv (eq .name "OTEL_RESOURCE_ATTRIBUTES_EXTRA") }}
{{-       $newValue := (printf "%s,%s" (get $resourceAttributesEnv "value") .value) }}
{{-       $resourceAttributesEnv = dict "name" "OTEL_RESOURCE_ATTRIBUTES" "value" $newValue }}
{{-     else }}
{{-       $allEnvs = append $allEnvs . }}
{{-     end }}
{{-   end }}
{{- end }}

{{- if $resourceAttributesEnv }}
{{-   $allEnvs = append $allEnvs $resourceAttributesEnv }}
{{- end }}

{{- tpl (toYaml $allEnvs) . }}
{{- end }}


{{/*
Get Kafka client env vars for a component that declares a `.kafka` block.
- legacy mode: literal KAFKA_ADDR/KAFKA_TOPIC (today's behavior).
- kafkaAccess mode (default): KAFKA_ADDR/KAFKA_TOPIC plus SASL/TLS env vars
  sourced from the Secret produced by a Strimzi KafkaAccess custom resource.
  When kafkaAccess.manageResources is true (default), that KafkaAccess is
  rendered by this chart (see templates/kafka-strimzi.yaml) and its Secret is
  always named "<component>-kafka-access"; with manageResources: false the
  Secret is bring-your-own, named via `.kafka.existingSecretName`. Every key
  besides bootstrap.servers is optional, since not every auth mode uses SASL
  or TLS.
*/}}
{{- define "otel-demo.pod.kafkaEnv" -}}
{{- if .kafka }}
{{- $mode := (.kafkaAccess).mode | default "kafkaAccess" }}
{{- if eq $mode "legacy" }}
- name: KAFKA_ADDR
  value: {{ (.kafkaAccess.legacy).bootstrapServers | default "kafka:9092" | quote }}
- name: KAFKA_TOPIC
  value: {{ (.kafkaAccess.legacy).topic | default "orders" | quote }}
{{- else }}
{{- /* Plain `| default true` would coerce an explicit `false` back to true,
since sprig's default treats any falsy value as unset. */}}
{{- $manageResources := true }}
{{- if hasKey (.kafkaAccess | default dict) "manageResources" }}
{{- $manageResources = .kafkaAccess.manageResources }}
{{- end }}
{{- $secret := .kafka.existingSecretName }}
{{- if $manageResources }}
{{- $secret = printf "%s-kafka-access" .name }}
{{- end }}
{{- $secret = required (printf "components.%s.kafka.existingSecretName is required when kafkaAccess.mode is \"kafkaAccess\" and kafkaAccess.manageResources is false" .name) $secret }}
- name: KAFKA_TOPIC
  value: {{ .kafkaAccess.topic | default "orders" | quote }}
- name: KAFKA_ADDR
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: bootstrap.servers
- name: KAFKA_PROTOCOL
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: security.protocol
      optional: true
- name: KAFKA_SASL_MECHANISM
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: sasl.mechanism
      optional: true
- name: KAFKA_SASL_USERNAME
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: username
      optional: true
- name: KAFKA_SASL_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: password
      optional: true
- name: KAFKA_SASL_JAAS_CONFIG
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: sasl.jaas.config
      optional: true
- name: KAFKA_SSL_TRUSTSTORE_CRT
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: ssl.truststore.crt
      optional: true
{{- end }}
{{- end }}
{{- end }}

{{/*
Get Kafka wait-for initContainer for a component that declares a `.kafka`
block. Only emitted in legacy mode: SASL/TLS connections make a bare TCP
`nc -z` probe meaningless, and all three Kafka clients already retry/back off
on connect failure, so kafkaAccess mode relies on that instead.
*/}}
{{- define "otel-demo.pod.kafkaInitContainer" -}}
{{- if .kafka }}
{{- $mode := (.kafkaAccess).mode | default "kafkaAccess" }}
{{- if eq $mode "legacy" }}
{{- $bootstrapServers := (.kafkaAccess.legacy).bootstrapServers | default "kafka:9092" }}
- name: wait-for-kafka
  image: busybox:latest
  command: ["sh", "-c", "until nc -z -v -w30 {{ $bootstrapServers | replace `:` ` ` }}; do echo waiting for kafka; sleep 2; done;"]
{{- end }}
{{- end }}
{{- end }}

{{/*
Get Postgres client env vars for a component that declares a `.postgres` block.
- legacy mode: literal DB_CONNECTION_STRING from `.postgres.legacy` (today's
  behavior, unchanged).
- cnpg mode (default): DB_HOST/DB_PORT/DB_USER/DB_PASSWORD/DB_NAME sourced via
  secretKeyRef from the Secret CNPG generates for its Cluster's app user
  (`.postgres.existingSecretName`, defaulting to "<postgresAccess.cnpg.clusterName>-app"),
  then DB_CONNECTION_STRING is built from those via Kubernetes $(VAR) env
  expansion, shaped by `.postgres.format` ("dotnet" | "uri" | "libpq").
*/}}
{{- define "otel-demo.pod.postgresEnv" -}}
{{- if .postgres }}
{{- $mode := (.postgresAccess).mode | default "cnpg" }}
{{- if eq $mode "legacy" }}
- name: DB_CONNECTION_STRING
  value: {{ required (printf "components.%s.postgres.legacy is required when postgresAccess.mode is \"legacy\"" .name) .postgres.legacy | quote }}
{{- else }}
{{- $secret := .postgres.existingSecretName | default (printf "%s-app" ((.postgresAccess.cnpg).clusterName | default "postgresql")) }}
- name: DB_HOST
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: host
- name: DB_PORT
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: port
- name: DB_USER
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: username
- name: DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: password
- name: DB_NAME
  valueFrom:
    secretKeyRef:
      name: {{ $secret }}
      key: dbname
- name: DB_CONNECTION_STRING
  value: {{ include "otel-demo.pod.postgresConnectionString" . | quote }}
{{- end }}
{{- end }}
{{- end }}

{{- define "otel-demo.pod.postgresConnectionString" -}}
{{- $format := required (printf "components.%s.postgres.format is required when postgresAccess.mode is \"cnpg\"" .name) .postgres.format }}
{{- if eq $format "uri" -}}
postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_NAME)?sslmode=prefer
{{- else if eq $format "libpq" -}}
host=$(DB_HOST) port=$(DB_PORT) user=$(DB_USER) password=$(DB_PASSWORD) dbname=$(DB_NAME) sslmode=prefer
{{- else if eq $format "dotnet" -}}
Host=$(DB_HOST);Username=$(DB_USER);Password=$(DB_PASSWORD);Database=$(DB_NAME)
{{- else -}}
{{ fail (printf "components.%s.postgres.format %q is not one of dotnet|uri|libpq" .name $format) }}
{{- end -}}
{{- end }}

{{/*
Get Pod ports
*/}}
{{- define "otel-demo.pod.ports" -}}
{{- if .ports }}
{{-   range $port := .ports }}
- containerPort: {{ $port.value }}
  name: {{ $port.name}}
{{-   end }}
{{- end }}
{{- if .service }}
{{-   if .service.port }}
- containerPort: {{.service.port}}
  name: service
{{-   end }}
{{- end }}
{{- end }}
