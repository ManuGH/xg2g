export function reloadWindowLocation(): void {
  if (typeof window !== 'undefined') {
    window.location.reload();
  }
}
