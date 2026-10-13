package patch

import (
	"bytes"
	"errors"
)

// This adapter changes the request representation, not the publisher's prompt.
// The identical source template is shared by the 4B, 12B and 27B checkpoints.
// English defaults let llama.cpp inspect its synthetic startup messages;
// callers select both language codes explicitly for actual translation.
const translateGemmaInput = `{# Temper translategemma-text-input-v1: preserve the publisher's rendered prompt. #}
{%- set translation_source = source_lang_code | default('en') -%}
{%- set translation_target = target_lang_code | default('en') -%}
{%- set translation_input = namespace(messages=[]) -%}
{%- for translation_message in messages -%}
    {%- if translation_message['role'] == 'user' and translation_message['content'] is string -%}
        {%- set translation_message = {'role': 'user', 'content': [{'type': 'text', 'source_lang_code': translation_source, 'target_lang_code': translation_target, 'text': translation_message['content']}]} -%}
    {%- endif -%}
    {%- set translation_input.messages = translation_input.messages + [translation_message] -%}
{%- endfor -%}
{%- set messages = translation_input.messages -%}
`

func translateGemmaTextInput(source []byte) ([]byte, error) {
	if bytes.Contains(source, []byte("Temper "+TranslateGemmaTextInputV1)) ||
		bytes.Count(source, []byte(`{%- set content = message["content"][0] -%}`)) != 1 {
		return nil, errors.New("TranslateGemma input transform requires one unadapted structured-content template")
	}
	return append([]byte(translateGemmaInput), source...), nil
}
