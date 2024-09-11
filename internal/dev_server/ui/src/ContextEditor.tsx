import {
  Label,
  TextField,
  TextArea,
  Text,
  FieldError,
} from '@launchpad-ui/components';
import { Stack } from '@launchpad-ui/core';

type Props = {
  context: string;
  setContext: (context: string) => void;
};

export function ContextEditor({ context, setContext }: Props) {
  return (
    <Stack gap="3">
      <Label style={{ fontSize: '1rem', fontWeight: 'bold' }}>Context</Label>
      <TextField
        value={context}
        onChange={setContext}
        validate={(value) => {
          try {
            JSON.parse(value);
            return null;
          } catch (err) {
            if (err instanceof Error) {
              return `Unable to parse value as JSON: ${err.toString()}`;
            } else {
              return `Unable to parse value as JSON: unknown parse error`;
            }
          }
        }}
        style={{
          flexGrow: 1,
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <TextArea
          style={{
            fontFamily: 'monospace',
            flexGrow: 1,
            minHeight: '18.75rem',
            backgroundColor: 'var(--lp-color-bg-ui-secondary)',
          }}
        />
        <Text slot="description">Edit the context as JSON</Text>
        <FieldError />
      </TextField>
    </Stack>
  );
}
