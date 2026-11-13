import * as Select from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";
export function Dropdown({
  id,
  label,
  value,
  options,
  onChange,
  disabled = false,
  placeholder,
}: {
  id?: string;
  label: string;
  value: string;
  options: { value: string; label: string; description?: string }[];
  onChange: (value: string) => void;
  disabled?: boolean;
  placeholder?: string;
}) {
  const selected = options.find((option) => option.value === value);
  return (
    <Select.Root
      value={placeholder && !value ? "" : JSON.stringify(value)}
      onValueChange={(encoded) => onChange(JSON.parse(encoded))}
      disabled={disabled}
    >
      <Select.Trigger
        id={id}
        aria-label={label}
        title={
          selected
            ? [selected.label, selected.description].filter(Boolean).join(" · ")
            : undefined
        }
        className="dropdown-trigger"
      >
        <Select.Value placeholder={placeholder}>{selected?.label}</Select.Value>
        <Select.Icon>
          <ChevronDown size={16} aria-hidden="true" />
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
                aria-label={[option.label, option.description]
                  .filter(Boolean)
                  .join(" · ")}
              >
                <Select.ItemText>
                  <span className="dropdown-item-label">{option.label}</span>
                  {option.description && (
                    <span className="dropdown-item-description">
                      {option.description}
                    </span>
                  )}
                </Select.ItemText>
                <Select.ItemIndicator>
                  <Check size={15} aria-hidden="true" />
                </Select.ItemIndicator>
              </Select.Item>
            ))}
          </Select.Viewport>
        </Select.Content>
      </Select.Portal>
    </Select.Root>
  );
}
