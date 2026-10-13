package main

import (
	"strings"
	"testing"
)

// Os horários da agenda nascem PROVISÓRIOS nos DEFAULTs da migration
// 20261013120000 e passam a ser do dono no primeiro `PUT /agenda/settings`.
// O seed roda a cada deploy; se reescrevesse a linha, desfaria a confirmação do
// dono na manhã seguinte, sem erro e sem linha de auditoria.
//
// Este teste não toca em banco: confere a forma do SQL, para a regra valer em
// `go test` comum. A prova contra o banco está em agenda_integration_test.go.
func TestSeedDaAgendaNuncaReescreveOsHorarios(t *testing.T) {
	sql := strings.Join(strings.Fields(sqlConfiguracaoDaAgenda), " ")

	if strings.Contains(strings.ToUpper(sql), "DO UPDATE") {
		t.Fatalf("a etapa agenda_settings faz DO UPDATE — reescreveria os horários que o dono confirmou:\n%s", sql)
	}
	if !strings.Contains(sql, "ON CONFLICT (property_id) DO NOTHING") {
		t.Fatalf("a etapa agenda_settings não absorve a linha existente com DO NOTHING:\n%s", sql)
	}

	// Só `property_id`: os valores provisórios têm UMA fonte, os DEFAULTs da
	// migration. Uma coluna a mais aqui seria a segunda cópia de "check-in às
	// 14:00", e as duas divergiriam no dia em que só uma fosse corrigida.
	if !strings.Contains(sql, "INSERT INTO agenda_settings (property_id) VALUES ($1)") {
		t.Fatalf("a etapa agenda_settings grava mais que property_id — os horários provisórios são os DEFAULTs da migration:\n%s", sql)
	}
}

// O preenchimento da agenda tem de existir e de rodar DEPOIS dos horários: os
// eventos automáticos saem da linha que `agenda_settings` garante. Rodando
// antes, o módulo da agenda criaria a linha por conta própria e a etapa dos
// horários contaria "inalterada" num banco novo — a contagem mentiria sobre
// quem criou o quê.
func TestPreenchimentoDaAgendaVemDepoisDosHorarios(t *testing.T) {
	posicao := map[string]int{}
	for i, e := range etapas() {
		if _, repetida := posicao[e.nome]; repetida {
			t.Fatalf("etapa %q aparece duas vezes", e.nome)
		}
		posicao[e.nome] = i
	}
	horarios, temHorarios := posicao["agenda_settings"]
	preenchimento, temPreenchimento := posicao["agenda_das_reservas_confirmadas"]
	if !temHorarios || !temPreenchimento {
		t.Fatalf("faltam etapas da agenda: agenda_settings=%v, agenda_das_reservas_confirmadas=%v — "+
			"sem o preenchimento, as reservas confirmadas antes da agenda nunca entram nela", temHorarios, temPreenchimento)
	}
	if preenchimento < horarios {
		t.Fatalf("agenda_das_reservas_confirmadas (%d) roda antes de agenda_settings (%d)", preenchimento, horarios)
	}
}
