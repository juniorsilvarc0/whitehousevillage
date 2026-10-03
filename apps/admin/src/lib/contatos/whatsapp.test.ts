import { describe, expect, it } from "vitest";

import { linkDoWhatsApp, mensagemDaReserva, primeiroNome } from "./whatsapp";

describe("linkDoWhatsApp", () => {
  it("abre a conversa com o número do contato, só dígitos", () => {
    expect(linkDoWhatsApp("+5586994259816")).toBe("https://wa.me/5586994259816");
  });

  it("leva a mensagem codificada", () => {
    expect(linkDoWhatsApp("+5586994259816", "Olá, Ana! WH-2026-0001")).toBe(
      "https://wa.me/5586994259816?text=Ol%C3%A1%2C%20Ana!%20WH-2026-0001",
    );
  });

  it("não oferece link para telefone ausente ou fora de E.164", () => {
    expect(linkDoWhatsApp(null)).toBeNull();
    expect(linkDoWhatsApp("")).toBeNull();
    expect(linkDoWhatsApp("(86) 99425-9816")).toBeNull();
  });
});

describe("mensagemDaReserva", () => {
  it("cumprimenta pelo primeiro nome e cita o código", () => {
    expect(mensagemDaReserva("  José Roberto da Silva ", "WH-2026-0001")).toBe(
      "Olá, José! Aqui é da White House Village, sobre a sua reserva WH-2026-0001.",
    );
    expect(primeiroNome("")).toBe("");
  });
});
