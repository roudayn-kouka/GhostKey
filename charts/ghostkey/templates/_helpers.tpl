{{/*
Expand the name of the chart.
*/}}
{{- define "ghostkey.name" -}}
ghostkey
{{- end }}

{{/*
Create a default fully qualified app name.
Truncated to 63 characters because some Kubernetes name fields are limited to this.
*/}}
{{- define "ghostkey.fullname" -}}
{{- $name := include "ghostkey.name" . }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "ghostkey.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "ghostkey.labels" -}}
app.kubernetes.io/name: {{ include "ghostkey.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ include "ghostkey.chart" . }}
{{- end }}

{{/*
Selector labels (used in matchLabels).
*/}}
{{- define "ghostkey.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ghostkey.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Certificate secret name.
*/}}
{{- define "ghostkey.certSecretName" -}}
{{- if eq .Values.certificates.mode "provided" -}}
{{ .Values.certificates.provided.secretName }}
{{- else -}}
ghostkey-webhook-certs
{{- end -}}
{{- end -}}

