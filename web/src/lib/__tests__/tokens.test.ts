import { describe, it, expect, beforeEach } from "vitest";

// Test localStorage key contract
describe("token keys", () => {
  beforeEach(() => localStorage.clear());

  it("stores refresh token in localStorage", () => {
    localStorage.setItem("m2a_refresh_token", "tok");
    expect(localStorage.getItem("m2a_refresh_token")).toBe("tok");
  });

  it("is distinct from sessionStorage", () => {
    localStorage.setItem("m2a_refresh_token", "local");
    sessionStorage.setItem("m2a_refresh_token", "session");
    expect(localStorage.getItem("m2a_refresh_token")).toBe("local");
    expect(sessionStorage.getItem("m2a_refresh_token")).toBe("session");
  });
});
