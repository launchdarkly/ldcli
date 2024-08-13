const API_BASE = import.meta.env.PROD ? '' : '/api';
export const apiRoute = (pathname: string) => `${API_BASE}${pathname}`;
