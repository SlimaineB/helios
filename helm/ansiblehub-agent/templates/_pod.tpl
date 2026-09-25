{{- define "ansiblehub-agent.pod" -}}
metadata:
  labels:
    app.kubernetes.io/name: {{ include "ansiblehub-agent.name" . }}
    app.kubernetes.io/instance: {{ .Release.Name }}
    app: ansiblehub-agent
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
  securityContext:
    {{- toYaml .Values.securityContext | nindent 4 }}
  containers:
    - name: agent
      image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
      imagePullPolicy: {{ .Values.image.pullPolicy }}
      env:
        - name: CONFIG_FILE
          value: /etc/ansiblehub/agent.yaml
      volumeMounts:
        - name: config
          mountPath: /etc/ansiblehub
          readOnly: true
        - name: state
          mountPath: {{ .Values.config.stateDir }}
      resources:
        {{- toYaml .Values.resources | nindent 8 }}
  volumes:
    - name: config
      secret:
        secretName: {{ include "ansiblehub-agent.configSecret" . }}
    {{- include "ansiblehub-agent.stateVolume" . | nindent 4 }}
{{- end }}