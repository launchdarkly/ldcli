export type FlagVariation = {
  _id: string;
  value: object | string | number | boolean;
  description?: string;
  name?: string;
};

export type ApiFlag = {
  key: string;
  variations: FlagVariation[];
};

type Links = {
  next?: { href: string };
};

export type FlagsApiResponse = {
  items: ApiFlag[];
  _links: Links;
};

import { apiRoute } from './util';
import { Environment } from './types';

export async function fetchEnvironments(projectKey: string): Promise<Environment[]> {
  const res = await fetch(apiRoute(`/dev/projects/${projectKey}/environments`));
  if (!res.ok) {
    throw new Error(`Got ${res.status}, ${res.statusText} from environments fetch`);
  }
  return res.json();
}
