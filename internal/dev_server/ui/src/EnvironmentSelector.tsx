import { useState, useMemo } from 'react';
import {
  Label,
  ListBox,
  ListBoxItem,
  Input,
  ProgressBar,
} from '@launchpad-ui/components';
import { Box, Stack } from '@launchpad-ui/core';
import { Environment } from './types';

type Props = {
  environments: Environment[] | null;
  selectedEnvironment: Environment | null;
  setSelectedEnvironment: (environment: Environment | null) => void;
};

export function EnvironmentSelector({
  environments,
  selectedEnvironment,
  setSelectedEnvironment,
}: Props) {
  const [searchQuery, setSearchQuery] = useState('');

  const filteredEnvironments = useMemo(() => {
    const query = searchQuery.toLowerCase();
    return (environments ?? []).filter(
      (env) =>
        env.name.toLowerCase().includes(query) ||
        env.key.toLowerCase().includes(query),
    );
  }, [environments, searchQuery]);

  return (
    <Stack gap="3">
      <Box display="flex" justifyContent="space-between" alignItems="center">
        <Label
          htmlFor="environmentSearch"
          style={{ fontSize: '1rem', fontWeight: 'bold' }}
        >
          Environments
        </Label>
      </Box>
      <Box display="flex" justifyContent="space-between" alignItems="center">
        <div style={{ position: 'relative', flexGrow: 1 }}>
          <Input
            id="environmentSearch"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value || '')}
            placeholder="Search environments..."
            aria-label="Search environments"
          />
          {environments === null && (
            <Box
              position="absolute"
              right="0.5rem"
              top="50%"
              transform="translateY(-50%)"
            >
              <ProgressBar size="small" aria-label="Loading environments" />
            </Box>
          )}
        </div>
        <span
          style={{
            whiteSpace: 'nowrap',
            flexShrink: 0,
            marginLeft: '0.5rem',
          }}
        >
          {selectedEnvironment?.name ? (
            selectedEnvironment.name
          ) : (
            <code>{selectedEnvironment?.key}</code>
          )}
        </span>
      </Box>

      <Box
        position="relative"
        height="12.5rem"
        overflow="auto"
        backgroundColor="var(--lp-color-bg-ui-secondary)"
        borderRadius="0.5rem"
        borderStyle="solid"
        borderWidth="0.0625rem"
        borderColor="var(--lp-color-border-ui-primary)"
      >
        <ListBox
          aria-label="Environments"
          selectionMode="single"
          selectedKeys={[selectedEnvironment?.key || '']}
          onSelectionChange={(keys) => {
            const selectedKey = String(Array.from(keys)[0]);
            const selected = environments?.find(
              (env) => env.key === selectedKey,
            );
            if (selected) {
              setSelectedEnvironment(selected);
            }
          }}
        >
          {filteredEnvironments.map((env) => (
            <ListBoxItem key={env.key} id={env.key}>
              {env.name}
            </ListBoxItem>
          ))}
        </ListBox>
      </Box>
    </Stack>
  );
}
