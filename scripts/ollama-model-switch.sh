#!/bin/bash
#
# ollama-model-switch.sh
# Prüft Ollama Session-Nutzung und gibt Modell-Entscheidung aus
#
# Usage: ./scripts/ollama-model-switch.sh <model-name> <usage-percent>
#
# Arguments:
#   model-name      - Name des zu verwendenden Modells (z.B. "claude-opus-4-7" oder "qwen3.5:35b-a3b-coding-nvfp4")
#   usage-percent   - Aktuelle Session-Nutzung in Prozent (0-100)
#
# Ausgabe:
#   Wählt das Modell basierend auf usage-percent:
#   - Bei < 80%: Nutzt das angegebene Modell (kann Sonnet/Opus sein)
#   - Bei >= 80%: Nutzt immer das lokale Ollama-Modell, AUSSER für Review-Steps
#
# Exit Codes:
#   0 - Modell wurde gewählt
#   1 - Fehler bei der Modell-Auswahl

set -e

OLLAMA_MODEL="qwen3.5:35b-a3b-coding-nvfp4"
OPUS_MODEL="claude-opus-4-7"
SONNET_MODEL="claude-sonnet-4-6"
THRESHOLD=80

# Funktion: Ist es ein Review-Step?
is_review_step() {
    local step_name="$1"
    case "$step_name" in
        bmad-testarch-test-review|bmad-code-review|bmad-security-review)
            return 0  # true
            ;;
        *)
            return 1   # false
            ;;
    esac
}

# Hauptlogik
main() {
    local requested_model="$1"
    local usage_percent="$2"
    local step_name="${3:-}"

    if [[ -z "$requested_model" ]] || [[ -z "$usage_percent" ]]; then
        echo "ERROR: Usage muss angegeben werden: $0 <model> <usage> [step-name]" >&2
        exit 1
    fi

    # Prüfen ob Review-Step
    if is_review_step "$step_name"; then
        # Review-Steps nutzen IMMER Opus, unabhängig von der Usage
        echo "$OPUS_MODEL"
        exit 0
    fi

    # Nicht-Review-Steps: Bei >= 80% Usage zum lokalen Ollama-Modell wechseln
    if [[ "$usage_percent" -ge "$THRESHOLD" ]]; then
        echo "$OLLAMA_MODEL"
    else
        echo "$requested_model"
    fi

    exit 0
}

main "$@"
