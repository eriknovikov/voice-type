import * as Select from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";
export function Dropdown({
  id,
  label,
  value,
  options,
  onChange,
  disabled = false,
}: {
  id?: string;
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  return (
    <Select.Root
      value={JSON.stringify(value)}
      onValueChange={(encoded) => onChange(JSON.parse(encoded))}
      disabled={disabled}
    >
      <Select.Trigger id={id} aria-label={label} className="dropdown-trigger">
        <Select.Value />
        <Select.Icon>
          <ChevronDown size={16} />
        </Select.Icon>
      </Select.Trigger>
      <Select.Portal>
        <Select.Content
          className="dropdown-menu"
          position="popper"
          sideOffset={6}
          collisionPadding={12}
        >
          <Select.Viewport>
            {options.map((option) => (
              <Select.Item
                className="dropdown-item"
                key={option.value}
                value={JSON.stringify(option.value)}
              >
                <Select.ItemText>{option.label}</Select.ItemText>
                <Select.ItemIndicator>
                  <Check size={15} />
                </Select.ItemIndicator>
              </Select.Item>
            ))}
          </Select.Viewport>
        </Select.Content>
      </Select.Portal>
    </Select.Root>
  );
}
