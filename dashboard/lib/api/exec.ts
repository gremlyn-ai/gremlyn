import { shieldApi } from "./client";

type ExecResponse = {
  output: string;
  error?: string;
  status: number;
};

export async function execCommand(command: string): Promise<ExecResponse> {
  return shieldApi<ExecResponse>("/api/v1/exec", {
    method: "POST",
    body: JSON.stringify({ command }),
  });
}
