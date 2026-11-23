defmodule DemoWeb.Card do
  use Phoenix.Component

  # bg-[#fafafa] in a comment outside a template is not a class.
  def card(assigns) do
    ~H"""
    <div class="card bg-base-200">{@title}</div>
    """
  end
end
