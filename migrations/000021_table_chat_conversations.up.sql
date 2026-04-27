--
-- PostgreSQL database dump
--

\restrict DzxQz8nmb5ciuhPjrdfaRgWFcGlHJUZ5SLcPpNcuJSDk0R0qh3bX0ds1GVr2Qvt

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: chat_conversations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.chat_conversations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    user_a_id uuid NOT NULL,
    user_b_id uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT chat_conv_ordered CHECK ((user_a_id < user_b_id))
);


--
-- Name: chat_conversations chat_conv_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_conversations
    ADD CONSTRAINT chat_conv_unique UNIQUE (user_a_id, user_b_id);


--
-- Name: chat_conversations chat_conversations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.chat_conversations
    ADD CONSTRAINT chat_conversations_pkey PRIMARY KEY (id);


--
-- Name: idx_chat_conversations_user_a; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_conversations_user_a ON public.chat_conversations USING btree (user_a_id);


--
-- Name: idx_chat_conversations_user_b; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_chat_conversations_user_b ON public.chat_conversations USING btree (user_b_id);


--
-- PostgreSQL database dump complete
--

\unrestrict DzxQz8nmb5ciuhPjrdfaRgWFcGlHJUZ5SLcPpNcuJSDk0R0qh3bX0ds1GVr2Qvt

