import { describe, expect, it } from "vitest";
import { localLoginInputError } from "./loginValidation";

describe("localLoginInputError", () => {
  it("allows security-factor login without a local password", () => {
    expect(localLoginInputError("admin", "", false)).toBe("");
  });

  it("still requires a configured local password", () => {
    expect(localLoginInputError("admin", "", true)).toBe("请输入账号与密码");
  });
});
