export function isTaskListRequest(parts: string[]): boolean {
  return parts.length === 0 || (parts.length === 1 && parts[0] === "list");
}

export function getSelectedValue(select: { getSelectedItem(): { value: string } | null } | null): string | null {
  return select?.getSelectedItem()?.value || null;
}
