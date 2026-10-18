{{/*
Expand the name of the chart.
*/}}
{{- define "tracium.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a fully qualified name using the release name.
*/}}
{{- define "tracium.fullname" -}}
{{- printf "%s" .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Image tag: component image.tag, then global.imageTag, then the chart's appVersion.
*/}}
{{- define "tracium.imageTag" -}}
{{- if .component.image.tag -}}
{{- .component.image.tag -}}
{{- else if .global.imageTag -}}
{{- .global.imageTag -}}
{{- else -}}
{{- .chart.AppVersion -}}
{{- end -}}
{{- end }}

{{/*
Common labels applied to every resource.
*/}}
{{- define "tracium.labels" -}}
app.kubernetes.io/name: {{ include "tracium.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end }}

{{/*
Selector labels for a named component (e.g. "collector", "api", "dashboard").
Usage: {{ include "tracium.selectorLabels" (dict "Release" .Release "component" "collector") }}
*/}}
{{- define "tracium.selectorLabels" -}}
app.kubernetes.io/name: {{ .component }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
DSNs expand $(CLICKHOUSE_PASSWORD) / $(POSTGRES_PASSWORD), which must be declared earlier in the container's env.
*/}}
{{- define "tracium.clickhouseDSN" -}}
clickhouse://default:$(CLICKHOUSE_PASSWORD)@{{ include "tracium.fullname" . }}-clickhouse:9000/tracium
{{- end }}
{{- define "tracium.postgresDSN" -}}
postgres://tracium:$(POSTGRES_PASSWORD)@{{ include "tracium.fullname" . }}-postgres:5432/tracium?sslmode=disable
{{- end }}
