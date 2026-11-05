{{- define "helios-agent.labels" -}}
app.kubernetes.io/name: {{ include "helios-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: agent
app.kubernetes.io/part-of: helios
{{- end }}

{{- define "helios-agent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "helios-agent.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "helios-agent.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "helios-agent.configFile" -}}
server: {{ .Values.config.server | quote }}
token: {{ .Values.config.token | quote }}
instance_id: {{ .Values.config.instanceId | quote }}
state_dir: {{ .Values.config.stateDir | quote }}
poll_interval: {{ .Values.config.pollInterval | quote }}
max_concurrent_apps: {{ .Values.config.maxConcurrentApps }}
autoheal: {{ .Values.config.autoheal }}
insecure_skip_verify: {{ .Values.config.insecureSkipVerify }}
ansible_bin: {{ .Values.config.ansibleBin | quote }}
{{- end }}

{{- define "helios-agent.configSecret" -}}
{{- if .Values.existingConfigSecret }}
{{- .Values.existingConfigSecret }}
{{- else }}
{{- printf "%s-config" (include "helios-agent.fullname" .) }}
{{- end }}
{{- end }}

{{- define "helios-agent.stateVolume" -}}
{{- if eq .Values.statePersistence.type "hostPath" }}
- name: state
  hostPath:
    path: {{ .Values.statePersistence.hostPath }}
    type: DirectoryOrCreate
{{- else if eq .Values.statePersistence.type "pvc" }}
- name: state
  persistentVolumeClaim:
    claimName: {{ .Values.statePersistence.pvcName }}
{{- else }}
- name: state
  emptyDir: {}
{{- end }}
{{- end }}