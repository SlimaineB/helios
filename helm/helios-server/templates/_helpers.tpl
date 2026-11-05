{{- define "helios-server.labels" -}}
app.kubernetes.io/name: {{ include "helios-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: server
app.kubernetes.io/part-of: helios
{{- end }}

{{- define "helios-server.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "helios-server.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "helios-server.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- define "helios-server.operatorSecret" -}}
{{- .Values.operatorAuth.existingSecret | default (printf "%s-operator" (include "helios-server.fullname" .)) }}
{{- end }}
