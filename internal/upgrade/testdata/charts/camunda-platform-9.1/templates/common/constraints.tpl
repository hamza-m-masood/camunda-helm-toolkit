{{/*
Fixture mirroring the real chart's constraints.tpl shapes.
*/}}
{{- if lt (int .Capabilities.HelmVersion.Major) 4 }}
{{- fail (printf "[camunda][error] Camunda chart 99.x requires Helm CLI v4 or later. Detected: %s" .Capabilities.HelmVersion.Version) -}}
{{- end }}

{{/*
camundaPlatform.keyRenamed
Usage:
{{ include "camundaPlatform.keyRenamed" (dict
  "condition" (.Values.doc.example.shouldBeIgnored)
  "oldName" "doc.example.shouldBeIgnored"
  "newName" "doc.example.ignored"
) }}
*/}}
{{- define "camundaPlatform.keyRenamed" }}
{{- end -}}

{{ include "camundaPlatform.keyRenamed" (dict
  "condition" (.Values.identity.keycloak)
  "oldName" "identity.keycloak"
  "newName" "identityKeycloak"
) }}

{{ include "camundaPlatform.keyRemoved" (dict
  "condition" (hasKey .Values.global.license "key")
  "oldName" "global.license.key"
) }}

{{ include "camundaPlatform.keyRemoved" (dict
  "condition" (and .Values.a.b .Values.c.d)
  "oldName" "compound.condition.key"
) }}

{{- define "camunda.constraints.warnings" }}
  {{- $orchestrationExtra := "orchestration.extraConfiguration" }}
  {{- if .Values.orchestration.enabled }}
    {{ include "camundaPlatform.keyDeprecated" (dict
      "condition" (ne (.Values.orchestration.logLevel | toString) "info")
      "oldName" "orchestration.logLevel" "migration" $orchestrationExtra) }}
    {{ include "camundaPlatform.keyDeprecated" (dict
      "condition" (not (empty .Values.orchestration.index.prefix))
      "oldName" "orchestration.index.prefix" "migration" $orchestrationExtra) }}
    {{ include "camundaPlatform.keyDeprecated" (dict
      "condition" (.Values.orchestration.history.retention.enabled)
      "oldName" "orchestration.history.retention.enabled" "migration" $orchestrationExtra) }}
    {{ include "camundaPlatform.keyDeprecated" (dict
      "condition" (not .Values.orchestration.security.authorizations.enabled)
      "oldName" "orchestration.security.authorizations.enabled" "migration" $orchestrationExtra) }}
  {{- end }}
{{- end -}}

{{- define "camundaPlatform.keyDeprecated" }}
  {{- $warningMessage := printf "[camunda][warning] DEPRECATION: %s is deprecated and will be removed in chart v100 (Camunda 9.2)." .oldName -}}
{{- end -}}
