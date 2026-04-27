--
-- PostgreSQL database dump
--

\restrict oFatVa5LdGir7MbeFaYxtYxM2gQWUlClU8sPuPmAADTK69OOFxcbCEl7Ol5Gkc8

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
-- Name: calls; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.calls (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    conversation_id uuid,
    caller_id uuid NOT NULL,
    callee_id uuid NOT NULL,
    status public.call_status DEFAULT 'RINGING'::public.call_status NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    started_at timestamp without time zone,
    ended_at timestamp without time zone,
    ended_by uuid,
    ended_reason character varying(50),
    client_request_id character varying(64),
    CONSTRAINT calls_not_same_user CHECK ((caller_id <> callee_id))
);


--
-- Name: calls calls_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.calls
    ADD CONSTRAINT calls_pkey PRIMARY KEY (id);


--
-- Name: idx_calls_callee_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_calls_callee_created ON public.calls USING btree (callee_id, created_at DESC);


--
-- Name: idx_calls_caller_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_calls_caller_created ON public.calls USING btree (caller_id, created_at DESC);


--
-- Name: idx_calls_client_request_id_uq; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_calls_client_request_id_uq ON public.calls USING btree (caller_id, client_request_id);


--
-- Name: idx_calls_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_calls_status_created ON public.calls USING btree (status, created_at DESC);


--
-- Name: calls calls_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.calls
    ADD CONSTRAINT calls_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.chat_conversations(id) ON DELETE SET NULL;


--
-- PostgreSQL database dump complete
--

\unrestrict oFatVa5LdGir7MbeFaYxtYxM2gQWUlClU8sPuPmAADTK69OOFxcbCEl7Ol5Gkc8

