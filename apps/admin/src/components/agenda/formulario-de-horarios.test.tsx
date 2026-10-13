import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { FormularioDeHorarios } from "@/components/agenda/formulario-de-horarios";
import { gravarHorarios } from "@/lib/agenda/acoes";
import type { ConfiguracaoDaAgenda } from "@/lib/agenda/tipos";

/**
 * Configurações → Agenda: a faixa "provisórios, a confirmar" enquanto
 * `provisional` for `true`, o salvar só com `settings:editar`, e quantos
 * eventos foram reprogramados depois de gravar.
 */

vi.mock("@/lib/agenda/acoes", () => ({ gravarHorarios: vi.fn() }));

const gravar = vi.mocked(gravarHorarios);

const PROVISORIA: ConfiguracaoDaAgenda = {
  timezone: "America/Sao_Paulo",
  today: "2026-11-20",
  checkin_time: "14:00",
  checkout_time: "11:00",
  cleaning_start: "11:00",
  cleaning_end: "14:00",
  business_hours: [
    { weekday: 1, start: "08:00", end: "20:00" },
    { weekday: 2, start: "08:00", end: "20:00" },
  ],
  slot_minutes: 30,
  buffer_minutes: 15,
  provisional: true,
  updated_at: "2026-10-13T10:00:00-03:00",
  updated_by: null,
  updated_by_name: null,
};

const FAIXA = /Horários provisórios, a confirmar/;

beforeEach(() => {
  gravar.mockReset();
});

describe("a faixa de provisório", () => {
  it("aparece enquanto provisional for true", () => {
    render(<FormularioDeHorarios configuracao={PROVISORIA} podeEditar />);
    expect(screen.getByText(FAIXA)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Confirmar e gravar" })).toBeTruthy();
  });

  it("não aparece quando os horários já foram confirmados", () => {
    render(<FormularioDeHorarios configuracao={{ ...PROVISORIA, provisional: false, updated_by_name: "Dono" }} podeEditar />);
    expect(screen.queryByText(FAIXA)).toBeNull();
    expect(screen.getByText(/por Dono/)).toBeTruthy();
  });

  it("depois de gravar, some — e a tela diz quantos eventos foram reprogramados", async () => {
    gravar.mockResolvedValue({
      ok: true,
      data: { configuracao: { ...PROVISORIA, provisional: false, checkin_time: "15:00" }, reprogramados: 12 },
    });
    render(<FormularioDeHorarios configuracao={PROVISORIA} podeEditar />);
    fireEvent.change(screen.getByLabelText(/^Check-in a partir de/), { target: { value: "15:00" } });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Confirmar e gravar" }));
    });
    await waitFor(() => expect(screen.getByRole("status").textContent).toMatch(/12 eventos automáticos foram para o horário novo/));
    expect(screen.queryByText(FAIXA)).toBeNull();
    expect(gravar.mock.calls[0]![0]).toMatchObject({ checkin_time: "15:00", slot_minutes: "30" });
  });
});

describe("salvar só com settings:editar", () => {
  it("sem a permissão: nada de botão, campos travados", () => {
    render(<FormularioDeHorarios configuracao={PROVISORIA} podeEditar={false} />);
    expect(screen.queryByRole("button", { name: /gravar/i })).toBeNull();
    expect((screen.getByLabelText(/^Check-in a partir de/) as HTMLInputElement).disabled).toBe(true);
    // A faixa continua: quem só lê também precisa saber que é provisório.
    expect(screen.getByText(FAIXA)).toBeTruthy();
  });
});

describe("a recusa da API cai no campo certo", () => {
  it("todas as violações de uma vez — inclusive o intervalo, pelo índice enviado", async () => {
    gravar.mockResolvedValue({
      ok: false,
      code: "VALIDATION_ERROR",
      message: "invalid",
      details: {
        checkin_time: "o próximo hóspede entra depois que a limpeza termina: a limpeza vai até 14:00.",
        "business_hours[1].end": "o fim tem de ser depois do início.",
      },
    });
    render(<FormularioDeHorarios configuracao={PROVISORIA} podeEditar />);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Confirmar e gravar" }));
    });
    await waitFor(() =>
      expect(document.getElementById("horarios-checkin-erro")?.textContent).toMatch(/a limpeza vai até 14:00/),
    );
    const terca = document.querySelector("[data-intervalo='1']");
    expect(terca?.textContent).toMatch(/o fim tem de ser depois do início/);
    expect(document.querySelector("[data-intervalo='0']")?.textContent).not.toMatch(/o fim tem de ser/);
  });
});
