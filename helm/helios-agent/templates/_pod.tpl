{{- define "helios-agent.pod" -}}
{{- if not .Values.existingConfigSecret }}
{{- if not .Values.config.token }}
{{- fail "config.token is required unless existingConfigSecret is set" }}
{{- end }}
{{- end }}
{{- if eq .Values.statePersistence.type "pvc" }}
{{- if not .Values.statePersistence.pvcName }}
{{- fail "statePersistence.pvcName is required when statePersistence.type=pvc" }}
{{- end }}
{{- end }}
metadata:
  labels:
    app.kubernetes.io/name: {{ include "helios-agent.name" . }}
    app.kubernetes.io/instance: {{ .Release.Name }}
    app: helios-agent
  {{- with .Values.podAnnotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  {{- with .Values.imagePullSecrets }}
  imagePullSecrets:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .Values.nodeSelector }}
  nodeSelector:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .Values.tolerations }}
  tolerations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .Values.affinity }}
  affinity:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  automountServiceAccountToken: false
  terminationGracePeriodSeconds: 35
  securityContext:
    {{- toYaml .Values.securityContext | nindent 4 }}
  containers:
    - name: agent
      image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
      imagePullPolicy: {{ .Values.image.pullPolicy }}
      env:
        - name: CONFIG_FILE
          value: /etc/helios/agent.yaml
      volumeMounts:
        - name: config
          mountPath: /etc/helios
          readOnly: true
        - name: state
          mountPath: {{ .Values.config.stateDir }}
      resources:
        {{- toYaml .Values.resources | nindent 8 }}
  volumes:
    - name: config
      secret:
        secretName: {{ include "helios-agent.configSecret" . }}
    {{- include "helios-agent.stateVolume" . | nindent 4 }}
{{- end }}