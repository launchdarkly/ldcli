import { useState, useEffect } from 'react';
import {
  Dialog,
  DialogTrigger,
  ModalOverlay,
  Modal,
  Form,
  Label,
  ButtonGroup,
  Button,
  Tooltip,
  TooltipTrigger,
  ProgressBar,
} from '@launchpad-ui/components';
import { Box, Stack, Inline } from '@launchpad-ui/core';
import { Environment } from './types';
import { EnvironmentSelector } from './EnvironmentSelector';
import { ContextEditor } from './ContextEditor';
import { ProjectEditButton } from './SubmitButton';

type Props = {
  projectKey: string;
  selectedEnvironment: Environment | null;
  setSelectedEnvironment: (selectedEnvironment: Environment | null) => void;
  sourceEnvironmentKey: string | null;
  context: string;
  updateProjectSettings: (
    newEnvironment: Environment | null,
    newContext: string,
  ) => Promise<void>;
};

export function ProjectEditor({
  projectKey,
  selectedEnvironment,
  setSelectedEnvironment,
  sourceEnvironmentKey,
  context,
  updateProjectSettings,
}: Props) {
  const [tempSelectedEnvironment, setTempSelectedEnvironment] =
    useState<Environment | null>(selectedEnvironment);
  const [tempContext, setTempContext] = useState<string>(context);
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    setTempSelectedEnvironment(selectedEnvironment);
    setTempContext(context);
  }, [selectedEnvironment, context]);

  const handleSubmit = async (close: () => void) => {
    setIsSubmitting(true);
    try {
      close();
      await updateProjectSettings(tempSelectedEnvironment, tempContext);
      setSelectedEnvironment(tempSelectedEnvironment);
    } catch (error) {
      console.error('Error submitting project settings:', error);
      // You might want to show an error message to the user here
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <DialogTrigger>
      <TooltipTrigger>
        <ProjectEditButton
          isSubmitting={isSubmitting}
          selectedEnvironment={selectedEnvironment}
        />
        <Tooltip>
          <span>Current environment. Click to update.</span>
        </Tooltip>
      </TooltipTrigger>
      <ModalOverlay>
        <Modal>
          <Dialog>
            {({ close }) => (
              <Form
                onSubmit={(e) => {
                  e.preventDefault();
                  handleSubmit(close);
                }}
                style={{
                  display: 'flex',
                  flexDirection: 'column',
                  padding: '1rem',
                }}
              >
                <Label
                  slot="title"
                  style={{ flexGrow: 0, paddingBottom: '2rem' }}
                >
                  Project Settings
                </Label>
                <Stack gap="3">
                  <EnvironmentSelector
                    projectKey={projectKey}
                    sourceEnvironmentKey={sourceEnvironmentKey}
                    selectedEnvironment={tempSelectedEnvironment}
                    setSelectedEnvironment={setTempSelectedEnvironment}
                  />
                  <Box
                    style={{
                      height: '1px',
                      backgroundColor: 'var(--lp-color-border-ui-primary)',
                      margin: '1rem 0',
                    }}
                  />
                  <ContextEditor
                    context={tempContext}
                    setContext={setTempContext}
                  />
                </Stack>
                <ButtonGroup style={{ justifyContent: 'flex-end' }}>
                  <Button
                    onPress={close}
                    variant="destructive"
                    isDisabled={isSubmitting}
                  >
                    Cancel
                  </Button>
                  <Button
                    variant="primary"
                    type="submit"
                    isDisabled={isSubmitting}
                  >
                    {isSubmitting ? (
                      <Inline gap="2" style={{ alignItems: 'center' }}>
                        <Box display="flex" alignItems="center">
                          <ProgressBar
                            aria-label="loading"
                            isIndeterminate
                            style={{ width: '16px', height: '16px' }}
                          />
                        </Box>
                        <span>Updating...</span>
                      </Inline>
                    ) : (
                      'Confirm'
                    )}
                  </Button>
                </ButtonGroup>
              </Form>
            )}
          </Dialog>
        </Modal>
      </ModalOverlay>
    </DialogTrigger>
  );
}
