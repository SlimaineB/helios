{{- define "ansiblehub-server.labels" -}}
app.kubernetes.io/name: {{ include "ansiblehub-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: server
app.kubernetes.io/part-of: ansiblehub
{{- end }}

{{- define "ansiblehub-server.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "ansiblehub-server.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "ansiblehub-server.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}