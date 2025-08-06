export interface Environment {
  key: string;
  name: string;
}

export interface SummaryEventPayload {
  kind: 'summary';
  features: object;
  [key: string]: any;
}

export interface FeatureEventPayload {
  kind: 'feature';
  creationDate: number;
  key: string;
  version: number;
  context: {
    kind: string;
    key: string;
    anonymous: boolean;
    _meta?: {
      redactedAttributes?: string[];
    };
  };
  variation: number;
  value: boolean | string | number | object;
  default: boolean | string | number | object;
  reason: {
    kind: string;
  };
}

export interface GenericEventPayload {
  kind: string;
  [key: string]: any;
}

export interface EventData {
  id: string;
  timestamp: number;
  data: SummaryEventPayload | FeatureEventPayload | GenericEventPayload;
}