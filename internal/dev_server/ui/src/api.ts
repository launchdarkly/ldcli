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
