export interface Environment {
  key: string;
  name: string;
}

export interface SummaryEventPayload {
  kind: 'summary';
  features: object;
  [key: string]: any;
}

export interface GenericEventPayload {
  kind: string;
  [key: string]: any;
}

export interface EventData {
  id: string;
  timestamp: number;
  data: SummaryEventPayload | GenericEventPayload;
}