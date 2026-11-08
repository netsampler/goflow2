{{- define "goflow2.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "goflow2.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 40 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "goflow2.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 40 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 40 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "goflow2.selectorLabels" -}}
app.kubernetes.io/name: {{ include "goflow2.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "goflow2.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | quote }}
{{ include "goflow2.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "goflow2.scheduling" -}}
{{- $root := .root -}}
{{- $settings := index $root.Values .component -}}
{{- $component := ternary "collector" .component (eq .component "goflow2") -}}
{{- with $root.Values.imagePullSecrets }}
imagePullSecrets:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with $root.Values.nodeSelector }}
nodeSelector:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- with $root.Values.tolerations }}
tolerations:
  {{- toYaml . | nindent 2 }}
{{- end }}
{{- if $root.Values.affinity }}
affinity:
  {{- toYaml $root.Values.affinity | nindent 2 }}
{{- else if ne $settings.antiAffinity "none" }}
affinity:
  podAntiAffinity:
    {{- if eq $settings.antiAffinity "hard" }}
    requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchLabels:
            {{- include "goflow2.selectorLabels" $root | nindent 12 }}
            app.kubernetes.io/component: {{ $component }}
        topologyKey: kubernetes.io/hostname
    {{- else }}
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchLabels:
              {{- include "goflow2.selectorLabels" $root | nindent 14 }}
              app.kubernetes.io/component: {{ $component }}
          topologyKey: kubernetes.io/hostname
    {{- end }}
{{- end }}
{{- end -}}

{{/* Render numeric XML values without scientific notation for byte limits. */}}
{{- define "goflow2.numericSettings" -}}
{{- range $name, $value := . }}
<{{ $name }}>{{ if eq (float64 $value) (floor (float64 $value)) }}{{ $value | int64 }}{{ else }}{{ $value }}{{ end }}</{{ $name }}>
{{- end }}
{{- end -}}

{{- define "goflow2.kafkaVoters" -}}
{{- $root := . -}}
{{- $voters := list -}}
{{- range $i := until (int .Values.kafka.replicaCount) -}}
{{- $voters = append $voters (printf "%d@%s-kafka-%d.%s-kafka-headless:9093" (add1 $i | int) (include "goflow2.fullname" $root) $i (include "goflow2.fullname" $root)) -}}
{{- end -}}
{{- join "," $voters -}}
{{- end -}}

{{- define "goflow2.volumeClaimTemplate" -}}
{{- $persistence := .persistence -}}
volumeClaimTemplates:
  - metadata:
      name: data
      {{- with $persistence.annotations }}
      annotations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
    spec:
      accessModes:
        {{- toYaml $persistence.accessModes | nindent 8 }}
      {{- if eq $persistence.storageClassName "-" }}
      storageClassName: ""
      {{- else if $persistence.storageClassName }}
      storageClassName: {{ $persistence.storageClassName | quote }}
      {{- end }}
      resources:
        requests:
          storage: {{ $persistence.size | quote }}
{{- end -}}
