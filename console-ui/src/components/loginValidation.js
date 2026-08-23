export function localLoginInputError(username, password, passwordRequired) {
  if (!username) return "请输入账号";
  if (passwordRequired && !password) return "请输入账号与密码";
  return "";
}
