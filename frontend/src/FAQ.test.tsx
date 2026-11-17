// @vitest-environment jsdom
import React from "react";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { FAQ } from "./FAQ";

afterEach(cleanup);

describe("FAQ", () => {
  it("renders closed accordions", () => {
    render(<FAQ />);
    const items =
      document.querySelectorAll<HTMLDetailsElement>("details.faq-item");
    expect(items.length).toBe(8);
    for (const item of items) expect(item.open).toBe(false);
  });

  it("answers common questions", () => {
    render(<FAQ />);
    expect(screen.getByText("Ctrl+Space")).toBeTruthy();
    expect(
      screen.getByText(/keeps Voice Type running in the background/),
    ).toBeTruthy();
    expect(screen.getByText(/Parakeet Unified English/)).toBeTruthy();
    expect(screen.getByText(/25 European languages/)).toBeTruthy();
    expect(screen.getByText(/never fall back to the cloud/)).toBeTruthy();
    expect(screen.getByText(/system keyring/)).toBeTruthy();
    expect(screen.getByText(/No speech detected/)).toBeTruthy();
    expect(screen.getByText(/Open diagnostics/)).toBeTruthy();
  });

  it("links to the key pages in a new tab", () => {
    render(<FAQ />);
    const openai = screen.getByRole("link", {
      name: "platform.openai.com/api-keys",
    });
    expect(openai.getAttribute("href")).toBe(
      "https://platform.openai.com/api-keys",
    );
    expect(openai.getAttribute("target")).toBe("_blank");
    expect(openai.getAttribute("rel")).toBe("noreferrer");
    const openrouter = screen.getByRole("link", {
      name: "openrouter.ai/keys",
    });
    expect(openrouter.getAttribute("href")).toBe("https://openrouter.ai/keys");
    expect(openrouter.getAttribute("target")).toBe("_blank");
    expect(openrouter.getAttribute("rel")).toBe("noreferrer");
  });

  it("shows Mac setup, output privacy, login and data help without Linux instructions", () => {
    render(<FAQ platform="darwin" />);
    expect(screen.getByText("Control+Option+Space")).toBeTruthy();
    expect(screen.getByText(/Microphone and Accessibility access/)).toBeTruthy();
    expect(screen.getByText(/No Screen Recording or Input Monitoring/)).toBeTruthy();
    expect(screen.getByText(/macOS Keychain/)).toBeTruthy();
    expect(screen.getByText(/despite advisory privacy markers/)).toBeTruthy();
    expect(screen.getByText(/Paste compatibility cannot be used with live insertion/)).toBeTruthy();
    expect(screen.getByText(/never overrides an OS disable/)).toBeTruthy();
    expect(screen.getByText("~/Library/Application Support/Voice Type/")).toBeTruthy();
    expect(screen.getByText(/This keeps your data/)).toBeTruthy();
    expect(document.body.textContent).not.toMatch(/IBus|GNOME|Wayland|X11|AppIndicator/);
  });

  it("uses the saved shortcut with platform labels", () => {
    const { rerender } = render(<FAQ platform="darwin" shortcut="Super+Shift+K" />);
    expect(screen.getByText("Command+Shift+K")).toBeTruthy();
    rerender(<FAQ platform="linux" shortcut="Ctrl+F8" />);
    expect(screen.getByText("Ctrl+F8")).toBeTruthy();
    expect(screen.getByText(/IBus is running/)).toBeTruthy();
    expect(screen.queryByText(/Which macOS permissions/)).toBeNull();
  });
});
